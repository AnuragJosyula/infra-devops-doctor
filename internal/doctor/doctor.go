// Package doctor scans the infrastructure graph against a built-in rule set and
// reports security, reliability, cost, and best-practice findings. Pure graph
// logic — no network calls.
package doctor

import (
	"fmt"
	"strings"

	"github.com/inframap/inframap/internal/graph"
)

// Severity levels, ordered.
const (
	SevCritical = "critical"
	SevHigh     = "high"
	SevMedium   = "medium"
	SevLow      = "low"
)

// Finding is one detected issue on one node.
type Finding struct {
	ID       string `json:"id"`
	NodeID   string `json:"node_id"`
	NodeName string `json:"node_name"`
	NodeType string `json:"node_type"`
	Severity string `json:"severity"`
	Category string `json:"category"` // security | reliability | cost | best_practice
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Fix      string `json:"fix"`
}

// Scan runs every rule against the graph and returns all findings.
func Scan(g *graph.Graph) []Finding {
	findings := []Finding{}
	nodes := g.NodeSlice()
	edges := g.EdgeSlice()

	// index: node id → has any non-contains edge; node id → attached roles
	connected := map[string]bool{}
	hasRole := map[string]bool{}
	for _, e := range edges {
		if e.Type != graph.EdgeContains {
			connected[e.Source] = true
			connected[e.Target] = true
		}
		if e.Type == graph.EdgeAttachedTo {
			if src := g.GetNode(e.Source); src != nil && strings.Contains(src.Type, "iam") {
				hasRole[e.Target] = true
			}
		}
	}

	add := func(n *graph.Node, sev, cat, title, detail, fix string) {
		findings = append(findings, Finding{
			ID:       fmt.Sprintf("%s/%s", n.ID, strings.ReplaceAll(strings.ToLower(title), " ", "-")),
			NodeID:   n.ID, NodeName: n.Name, NodeType: n.Type,
			Severity: sev, Category: cat, Title: title, Detail: detail, Fix: fix,
		})
	}

	for _, n := range nodes {
		m := n.Metadata
		get := func(k string) string { return strings.ToLower(m[k]) }

		switch n.Type {
		case "s3_bucket", "gcs_bucket", "storage_account":
			if get("public") == "true" || strings.Contains(get("access"), "public") {
				add(n, SevCritical, "security", "Bucket is publicly accessible",
					"Anyone on the internet can read this bucket's contents.",
					"Enable Block Public Access and review the bucket policy/ACLs.")
			}
			enc := get("encryption")
			if enc == "" || enc == "none" || enc == "false" {
				add(n, SevHigh, "security", "Bucket not encrypted at rest",
					"Objects are stored without server-side encryption.",
					"Enable default encryption (SSE-S3/AES256 or SSE-KMS).")
			}
			if v := get("versioning"); v != "" && v != "enabled" {
				add(n, SevLow, "best_practice", "Versioning disabled",
					"Deleted or overwritten objects cannot be recovered.",
					"Enable bucket versioning.")
			}

		case "security_group", "nsg", "firewall":
			in := get("inbound")
			if strings.Contains(in, "0.0.0.0/0") {
				if strings.Contains(in, "22") || strings.Contains(in, "3389") {
					add(n, SevCritical, "security", "SSH/RDP open to the internet",
						"Port 22/3389 accepts connections from any IP (0.0.0.0/0). Brute-force target.",
						"Restrict the source to a VPN/bastion CIDR, or use SSM Session Manager.")
				} else if strings.Contains(in, "3306") || strings.Contains(in, "5432") || strings.Contains(in, "6379") || strings.Contains(in, "27017") {
					add(n, SevCritical, "security", "Database port open to the internet",
						"A database port accepts connections from any IP.",
						"Allow only application security groups / private subnets.")
				} else if !onlyWebPorts(in) {
					add(n, SevHigh, "security", "Wide-open inbound rule",
						"Inbound traffic from 0.0.0.0/0 on non-web ports.",
						"Scope the rule to known CIDRs or security groups.")
				}
			}

		case "rds", "cloud_sql", "sql_database":
			if get("publicly_accessible") == "true" {
				add(n, SevCritical, "security", "Database is publicly accessible",
					"The DB endpoint resolves to a public IP.",
					"Disable public accessibility; connect via private subnets/VPN.")
			}
			if get("encrypted") == "false" {
				add(n, SevHigh, "security", "Database storage not encrypted",
					"Data at rest is unencrypted.",
					"Enable storage encryption (requires snapshot+restore on AWS).")
			}
			if get("multi_az") != "true" && get("role") != "read-replica" {
				add(n, SevMedium, "reliability", "No Multi-AZ / HA standby",
					"A single-AZ failure takes this database down.",
					"Enable Multi-AZ (or an HA configuration) for production databases.")
			}

		case "ec2", "vm", "gce_instance":
			if n.Status == graph.StatusStopped {
				add(n, SevLow, "cost", "Stopped instance still incurs storage cost",
					"Stopped instances keep paying for attached volumes and IPs.",
					"Terminate if unused, or snapshot and delete volumes.")
			}
			if m["public_ip"] != "" {
				add(n, SevMedium, "security", "Instance has a public IP",
					"Directly reachable from the internet if any SG rule allows it.",
					"Prefer private subnets behind a load balancer or NAT.")
			}
			if !hasRole[n.ID] && n.Provider != "docker" {
				add(n, SevLow, "best_practice", "No IAM role attached",
					"Credentials are likely hardcoded or absent for API access.",
					"Attach an instance profile with least-privilege policies.")
			}

		case "iam_role", "iam_policy":
			p := get("policies")
			if strings.Contains(p, "fullaccess") || strings.Contains(p, "*:*") || strings.Contains(p, "administratoraccess") {
				add(n, SevHigh, "security", "Overly permissive IAM policy",
					fmt.Sprintf("Role carries broad permissions: %s", m["policies"]),
					"Replace FullAccess/admin policies with least-privilege, action-scoped ones.")
			}

		case "alb", "elb", "load_balancer":
			l := get("listeners")
			if strings.Contains(l, "http:80") && !strings.Contains(l, "redirect") {
				add(n, SevMedium, "security", "Plain-HTTP listener",
					"Port 80 serves traffic without TLS.",
					"Redirect HTTP→HTTPS at the listener and use an ACM/managed certificate.")
			}

		case "cloudfront", "cdn":
			if get("ssl") == "" {
				add(n, SevMedium, "security", "No TLS certificate configured",
					"Distribution may serve over default/insecure settings.",
					"Attach a managed TLS certificate and enforce HTTPS.")
			}

		case "volume", "ebs_volume", "disk":
			if get("encrypted") == "false" {
				add(n, SevHigh, "security", "Unencrypted volume",
					"Block storage is not encrypted at rest.",
					"Enable encryption; migrate data to an encrypted volume.")
			}

		case "container":
			if get("privileged") == "true" {
				add(n, SevCritical, "security", "Privileged container",
					"Container has full access to the host kernel.",
					"Drop privileged mode; grant only the specific capabilities needed.")
			}
			if strings.HasSuffix(get("image"), ":latest") {
				add(n, SevLow, "best_practice", "Container uses :latest tag",
					"Deploys are not reproducible; rollbacks are unreliable.",
					"Pin images to a version or digest.")
			}
		}

		// generic status rules
		switch n.Status {
		case graph.StatusDegraded:
			add(n, SevHigh, "reliability", "Resource is degraded",
				"The provider reports this resource in a degraded state.",
				"Inspect provider console/logs for the failing component.")
		case "error", "unhealthy":
			add(n, SevCritical, "reliability", "Resource is unhealthy",
				"The resource is failing health checks.",
				"Investigate immediately — dependent resources may be impacted.")
		}

		// orphan rule: leaf resource with no relationships at all
		if !connected[n.ID] && n.Parent == "" && n.Type != "region" && n.Type != "docker_daemon" && n.Type != "resource_group" {
			add(n, SevLow, "cost", "Orphaned resource",
				"No relationships to anything else — possibly forgotten.",
				"Verify it is still needed; delete to save cost.")
		}
	}

	return findings
}

// onlyWebPorts reports whether an inbound rule string mentions 0.0.0.0/0 only
// alongside ports 80/443.
func onlyWebPorts(in string) bool {
	has80or443 := strings.Contains(in, "80") || strings.Contains(in, "443")
	other := false
	for _, p := range []string{"21", "22", "23", "25", "445", "3306", "3389", "5432", "6379", "8080", "9200", "27017"} {
		if strings.Contains(in, p) {
			other = true
		}
	}
	return has80or443 && !other
}
