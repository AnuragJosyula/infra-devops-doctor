// Package cost provides approximate monthly cost estimation for infrastructure
// resources using a static, offline pricing table (no API keys, no network calls).
// Figures are rough on-demand us-east-1 equivalents — good for relative comparison,
// not billing accuracy.
package cost

import (
	"strconv"
	"strings"

	"github.com/inframap/inframap/internal/graph"
)

// approximate $/month for compute instance sizes (AWS, Azure, GCP)
var instancePrices = map[string]float64{
	// AWS EC2
	"t3.nano": 3.8, "t3.micro": 7.6, "t3.small": 15.2, "t3.medium": 30.4,
	"t3.large": 60.7, "t3.xlarge": 121.5, "t2.micro": 8.5, "t2.small": 16.8,
	"m5.large": 70.1, "m5.xlarge": 140.2, "m5.2xlarge": 280.3,
	"c5.large": 62.1, "c5.xlarge": 124.1, "r5.large": 91.9, "r6g.large": 73.6,
	// AWS RDS
	"db.t3.micro": 12.4, "db.t3.small": 24.8, "db.t3.medium": 49.6,
	"db.t3.large": 99.3, "db.m5.large": 124.8, "db.r6g.large": 158.4,
	"db.r5.large": 175.2,
	// ElastiCache
	"cache.t3.micro": 12.4, "cache.t3.small": 24.8, "cache.t3.medium": 49.6,
	"cache.r6g.large": 149.0, "cache.m5.large": 113.2,
	// Azure VMs
	"standard_b1s": 7.6, "standard_b2s": 30.4, "standard_b2ms": 60.7,
	"standard_d2s_v3": 70.1, "standard_d4s_v3": 140.2, "standard_e2s_v3": 91.9,
	// GCP machine types
	"e2-micro": 6.1, "e2-small": 12.2, "e2-medium": 24.5,
	"e2-standard-2": 49.0, "n1-standard-1": 24.3, "n1-standard-2": 48.5,
	"n2-standard-2": 56.8,
}

// flat approximate $/month by resource type (things without a size dimension,
// or where usage dominates and we can only ballpark)
var flatPrices = map[string]float64{
	"nat_gateway": 32.9, "alb": 22.3, "elb": 18.3, "load_balancer": 22.3,
	"eip": 3.6, "s3_bucket": 5.0, "storage_account": 5.0, "gcs_bucket": 5.0,
	"cloudfront": 10.0, "cdn": 10.0, "route53": 0.5, "dns_record": 0,
	"lambda": 4.0, "cloud_function": 4.0, "function_app": 4.0,
	"cloudwatch": 3.0, "sns": 1.0, "sqs": 1.0,
	"ecs_cluster": 0, "vpc": 0, "vnet": 0, "subnet": 0, "subnetwork": 0,
	"security_group": 0, "internet_gateway": 0, "iam_role": 0, "region": 0,
	"aks": 73.0, "eks": 73.0, "gke": 73.0, // control plane
	"app_service": 55.0, "sql_server": 0, "resource_group": 0,
}

// Annotate estimates a monthly cost for every node in the graph, writes it to
// Node.Cost, and returns the total.
func Annotate(g *graph.Graph) float64 {
	var total float64
	for _, n := range g.NodeSlice() {
		c := estimate(n)
		n.Cost = c
		total += c
	}
	return total
}

func estimate(n *graph.Node) float64 {
	m := n.Metadata
	size := strings.ToLower(firstOf(m, "instance_type", "instance_class", "node_type", "vm_size", "machine_type", "tier"))

	switch n.Type {
	case "ec2", "vm", "gce_instance", "instance":
		c := lookup(size, 30.0)
		if n.Status == graph.StatusStopped {
			return 0 // stopped instances only pay storage; call it ~0
		}
		return c

	case "rds", "cloud_sql", "sql_database":
		c := lookup(size, 100.0)
		c += parseGiB(firstOf(m, "storage", "allocated_storage")) * 0.115
		if strings.EqualFold(m["multi_az"], "true") {
			c *= 2
		}
		return c

	case "elasticache", "redis", "memcached":
		c := lookup(size, 50.0)
		if nodes, err := strconv.Atoi(m["num_nodes"]); err == nil && nodes > 1 {
			c *= float64(nodes)
		}
		return c

	case "ecs_task", "fargate_task":
		// Fargate: vCPU $0.04048/h + GB $0.004445/h
		cpu := parseF(m["cpu"], 512) / 1024 // units → vCPU
		mem := parseF(m["memory"], 1024) / 1024
		return (cpu*0.04048 + mem*0.004445) * 730

	case "ecs_service", "deployment":
		return 0 // cost lives on the tasks

	case "volume", "ebs_volume", "disk":
		return parseGiB(firstOf(m, "size", "storage")) * 0.08

	case "container", "image", "network", "docker_daemon":
		return 0 // local docker
	}

	if size != "" {
		if p, ok := instancePrices[size]; ok {
			return p
		}
	}
	if p, ok := flatPrices[n.Type]; ok {
		return p
	}
	return 0
}

func lookup(size string, def float64) float64 {
	if p, ok := instancePrices[size]; ok {
		return p
	}
	return def
}

func firstOf(m map[string]string, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != "" {
			return v
		}
	}
	return ""
}

// parseGiB extracts a number from strings like "100 GiB gp3" or "50".
func parseGiB(s string) float64 {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(fields[0], 64)
	return v
}

func parseF(s string, def float64) float64 {
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return v
	}
	return def
}
