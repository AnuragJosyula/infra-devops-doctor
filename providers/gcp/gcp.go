// Package gcp discovers Google Cloud resources by shelling out to the `gcloud`
// CLI, using the machine's existing login and configured project.
package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/inframap/inframap/internal/graph"
	"github.com/inframap/inframap/internal/provider"
)

var _ provider.Provider = (*GCP)(nil)

type GCP struct{ project string }

// New returns the provider if gcloud is installed and a project is configured.
func New() (*GCP, error) {
	if _, err := exec.LookPath("gcloud"); err != nil {
		return nil, fmt.Errorf("gcloud CLI not found in PATH")
	}
	out, err := exec.Command("gcloud", "config", "get-value", "project").Output()
	project := strings.TrimSpace(string(out))
	if err != nil || project == "" || project == "(unset)" {
		return nil, fmt.Errorf("no gcloud project configured (run `gcloud init`)")
	}
	return &GCP{project: project}, nil
}

func (p *GCP) Name() string        { return "gcp" }
func (p *GCP) Description() string { return "Google Cloud Platform (via gcloud CLI login)" }
func (p *GCP) Watch(context.Context, chan<- graph.Event) error {
	return fmt.Errorf("gcp provider does not support live watching")
}

func (p *GCP) Discover(ctx context.Context) (*graph.Graph, error) {
	g := graph.New()
	projectID := "gcp-project-" + p.project
	g.AddNode(&graph.Node{
		ID: projectID, Type: "gcp_project", Provider: "gcp",
		Name: p.project, Status: graph.StatusActive, Group: "Project",
	})

	// each service is best-effort: a missing API/permission skips that service
	p.instances(ctx, g, projectID)
	p.networks(ctx, g, projectID)
	p.buckets(ctx, g, projectID)
	p.sql(ctx, g, projectID)

	if len(g.Nodes) <= 1 {
		return nil, fmt.Errorf("no GCP resources discovered in project %s", p.project)
	}
	return g, nil
}

func (p *GCP) run(ctx context.Context, v any, args ...string) error {
	args = append(args, "--format=json", "--project="+p.project)
	out, err := exec.CommandContext(ctx, "gcloud", args...).Output()
	if err != nil {
		return err
	}
	return json.Unmarshal(out, v)
}

func (p *GCP) instances(ctx context.Context, g *graph.Graph, parent string) {
	var items []struct {
		Name, Status, MachineType, Zone string
	}
	if p.run(ctx, &items, "compute", "instances", "list") != nil {
		return
	}
	for _, it := range items {
		id := "gce-" + it.Name
		g.AddNode(&graph.Node{
			ID: id, Type: "gce_instance", Provider: "gcp", Name: it.Name,
			Status: strings.ToLower(it.Status), Region: last(it.Zone),
			Metadata: map[string]string{"machine_type": last(it.MachineType)},
			Parent:   parent, Group: "Compute",
		})
		g.AddEdge(&graph.Edge{ID: parent + "->" + id, Source: parent, Target: id, Type: graph.EdgeContains})
	}
}

func (p *GCP) networks(ctx context.Context, g *graph.Graph, parent string) {
	var items []struct{ Name string }
	if p.run(ctx, &items, "compute", "networks", "list") != nil {
		return
	}
	for _, it := range items {
		id := "gcpnet-" + it.Name
		g.AddNode(&graph.Node{
			ID: id, Type: "gcp_network", Provider: "gcp", Name: it.Name,
			Status: graph.StatusActive, Parent: parent, Group: "Networking",
		})
		g.AddEdge(&graph.Edge{ID: parent + "->" + id, Source: parent, Target: id, Type: graph.EdgeContains})
	}
}

func (p *GCP) buckets(ctx context.Context, g *graph.Graph, parent string) {
	var items []struct {
		Name     string `json:"name"`
		Location string `json:"location"`
	}
	if p.run(ctx, &items, "storage", "buckets", "list") != nil {
		return
	}
	for _, it := range items {
		id := "gcs-" + it.Name
		g.AddNode(&graph.Node{
			ID: id, Type: "gcs_bucket", Provider: "gcp", Name: it.Name,
			Status: graph.StatusActive, Region: strings.ToLower(it.Location),
			Parent: parent, Group: "Storage",
		})
		g.AddEdge(&graph.Edge{ID: parent + "->" + id, Source: parent, Target: id, Type: graph.EdgeContains})
	}
}

func (p *GCP) sql(ctx context.Context, g *graph.Graph, parent string) {
	var items []struct {
		Name, State     string
		DatabaseVersion string `json:"databaseVersion"`
	}
	if p.run(ctx, &items, "sql", "instances", "list") != nil {
		return
	}
	for _, it := range items {
		id := "cloudsql-" + it.Name
		g.AddNode(&graph.Node{
			ID: id, Type: "cloud_sql", Provider: "gcp", Name: it.Name,
			Status:   strings.ToLower(it.State),
			Metadata: map[string]string{"engine": it.DatabaseVersion},
			Parent:   parent, Group: "Database",
		})
		g.AddEdge(&graph.Edge{ID: parent + "->" + id, Source: parent, Target: id, Type: graph.EdgeContains})
	}
}

func last(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}
