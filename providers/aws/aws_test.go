package aws

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/inframap/inframap/internal/doctor"
	"github.com/inframap/inframap/internal/graph"
)

// linkSecurityGroupRules is the core of dependency discovery: a rule saying
// "SG-web may reach SG-db" must become connects_to edges between the actual
// instances in those groups, which is what blast radius traverses.
func TestLinkSecurityGroupRules(t *testing.T) {
	g := graph.New()
	a := &AWS{}

	rules := []sgRule{
		{targetSG: "sg-db", sourceSG: "sg-web", port: "5432"},
	}
	members := map[string][]string{
		"sg-web": {"i-web1", "i-web2"},
		"sg-db":  {"db-primary"},
	}

	a.linkSecurityGroupRules(g, rules, members)

	edges := g.EdgeSlice()
	if len(edges) != 2 {
		t.Fatalf("expected 2 connects_to edges (2 web instances -> 1 db), got %d", len(edges))
	}
	for _, e := range edges {
		if e.Type != graph.EdgeConnectsTo {
			t.Errorf("edge %s: expected type %q, got %q", e.ID, graph.EdgeConnectsTo, e.Type)
		}
		if e.Target != "db-primary" {
			t.Errorf("edge %s: expected target db-primary, got %s", e.ID, e.Target)
		}
		if !strings.Contains(e.Label, "5432") {
			t.Errorf("edge %s: expected port in label, got %q", e.ID, e.Label)
		}
	}
}

// A group that references itself must not produce self-edges.
func TestLinkSecurityGroupRulesSkipsSelf(t *testing.T) {
	g := graph.New()
	a := &AWS{}

	a.linkSecurityGroupRules(g,
		[]sgRule{{targetSG: "sg-app", sourceSG: "sg-app", port: "all"}},
		map[string][]string{"sg-app": {"i-1", "i-2"}},
	)

	for _, e := range g.EdgeSlice() {
		if e.Source == e.Target {
			t.Errorf("self-edge created on %s", e.Source)
		}
	}
	if got := len(g.EdgeSlice()); got != 2 { // i-1<->i-2 both directions
		t.Errorf("expected 2 peer edges, got %d", got)
	}
}

// Rules referencing a group with no members produce nothing rather than panicking.
func TestLinkSecurityGroupRulesEmptyMembers(t *testing.T) {
	g := graph.New()
	a := &AWS{}
	a.linkSecurityGroupRules(g,
		[]sgRule{{targetSG: "sg-empty", sourceSG: "sg-also-empty", port: "80"}},
		map[string][]string{},
	)
	if got := len(g.EdgeSlice()); got != 0 {
		t.Errorf("expected no edges, got %d", got)
	}
}

func TestPortRange(t *testing.T) {
	cases := []struct {
		name string
		perm ec2types.IpPermission
		want string
	}{
		{"single port", ec2types.IpPermission{
			IpProtocol: aws.String("tcp"), FromPort: aws.Int32(22), ToPort: aws.Int32(22)}, "22"},
		{"range", ec2types.IpPermission{
			IpProtocol: aws.String("tcp"), FromPort: aws.Int32(8000), ToPort: aws.Int32(8100)}, "8000-8100"},
		{"all protocols", ec2types.IpPermission{IpProtocol: aws.String("-1")}, "all"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := portRange(tc.perm); got != tc.want {
				t.Errorf("portRange() = %q, want %q", got, tc.want)
			}
		})
	}
}

// formatIngress output is consumed by the doctor rules, so the shape matters:
// an open SSH rule must actually trigger a critical finding end to end.
func TestFormatIngressTriggersDoctorRule(t *testing.T) {
	perms := []ec2types.IpPermission{{
		IpProtocol: aws.String("tcp"),
		FromPort:   aws.Int32(22),
		ToPort:     aws.Int32(22),
		IpRanges:   []ec2types.IpRange{{CidrIp: aws.String("0.0.0.0/0")}},
	}}

	inbound := formatIngress(perms)
	if !strings.Contains(inbound, "0.0.0.0/0") || !strings.Contains(inbound, "22") {
		t.Fatalf("formatIngress() = %q, missing port or CIDR", inbound)
	}

	g := graph.New()
	g.AddNode(&graph.Node{
		ID: "sg-open", Type: "security_group", Provider: "aws", Name: "open-ssh",
		Status: graph.StatusActive, Parent: "vpc-1",
		Metadata: map[string]string{"inbound": inbound},
	})

	var found bool
	for _, f := range doctor.Scan(g) {
		if f.NodeID == "sg-open" && f.Severity == doctor.SevCritical &&
			strings.Contains(f.Title, "SSH") {
			found = true
		}
	}
	if !found {
		t.Error("open SSH security group did not produce a critical doctor finding")
	}
}

func TestArnTail(t *testing.T) {
	cases := map[string]string{
		"arn:aws:iam::123456789012:role/my-role":                "my-role",
		"arn:aws:ecs:us-east-1:123456789012:cluster/prod":       "prod",
		"arn:aws:lambda:us-east-1:123456789012:function:my-fn":  "my-fn",
		"plain-name": "plain-name",
	}
	for in, want := range cases {
		if got := arnTail(in); got != want {
			t.Errorf("arnTail(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAddEdgeGuards(t *testing.T) {
	g := graph.New()
	addEdge(g, "", "target", graph.EdgeContains)
	addEdge(g, "source", "", graph.EdgeContains)
	addEdge(g, "same", "same", graph.EdgeContains)
	if got := len(g.EdgeSlice()); got != 0 {
		t.Errorf("expected empty/self edges to be dropped, got %d edges", got)
	}

	addEdge(g, "a", "b", graph.EdgeContains)
	if got := len(g.EdgeSlice()); got != 1 {
		t.Errorf("expected 1 valid edge, got %d", got)
	}
}

func TestResolveRegions(t *testing.T) {
	a := &AWS{}
	a.cfg.Region = "us-east-1"

	got, err := a.resolveRegions(nil, "")
	if err != nil || len(got) != 1 || got[0] != "us-east-1" {
		t.Errorf("blank spec should use configured region, got %v (err %v)", got, err)
	}

	got, _ = a.resolveRegions(nil, "us-west-2, eu-west-1 ,")
	if len(got) != 2 || got[0] != "us-west-2" || got[1] != "eu-west-1" {
		t.Errorf("comma list not parsed correctly, got %v", got)
	}

	empty := &AWS{}
	if _, err := empty.resolveRegions(nil, ""); err == nil {
		t.Error("expected an error when no region is configured")
	}
}
