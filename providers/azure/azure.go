// Package azure discovers Azure resources by shelling out to the `az` CLI,
// using whatever login the local machine already has. No SDK, no stored creds.
package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/inframap/inframap/internal/graph"
	"github.com/inframap/inframap/internal/provider"
)

var _ provider.Provider = (*Azure)(nil)

type Azure struct{}

// New returns the provider if the az CLI is installed.
func New() (*Azure, error) {
	if _, err := exec.LookPath("az"); err != nil {
		return nil, fmt.Errorf("az CLI not found in PATH")
	}
	return &Azure{}, nil
}

func (a *Azure) Name() string        { return "azure" }
func (a *Azure) Description() string { return "Microsoft Azure (via az CLI login)" }
func (a *Azure) Watch(context.Context, chan<- graph.Event) error {
	return fmt.Errorf("azure provider does not support live watching")
}

// typeMap: Azure resource type → (graph type, group)
var typeMap = map[string][2]string{
	"microsoft.compute/virtualmachines":         {"vm", "Compute"},
	"microsoft.compute/disks":                   {"disk", "Storage"},
	"microsoft.network/virtualnetworks":         {"vnet", "Networking"},
	"microsoft.network/networksecuritygroups":   {"nsg", "Security"},
	"microsoft.network/publicipaddresses":       {"eip", "Networking"},
	"microsoft.network/loadbalancers":           {"load_balancer", "Networking"},
	"microsoft.network/networkinterfaces":       {"network", "Networking"},
	"microsoft.storage/storageaccounts":         {"storage_account", "Storage"},
	"microsoft.sql/servers":                     {"sql_server", "Database"},
	"microsoft.sql/servers/databases":           {"sql_database", "Database"},
	"microsoft.web/sites":                       {"app_service", "Compute"},
	"microsoft.web/serverfarms":                 {"app_service_plan", "Compute"},
	"microsoft.containerservice/managedclusters": {"aks", "Compute"},
	"microsoft.keyvault/vaults":                 {"key_vault", "Security"},
	"microsoft.dbforpostgresql/flexibleservers": {"cloud_sql", "Database"},
}

type azResource struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Type          string            `json:"type"`
	Location      string            `json:"location"`
	ResourceGroup string            `json:"resourceGroup"`
	Tags          map[string]string `json:"tags"`
}

func (a *Azure) Discover(ctx context.Context) (*graph.Graph, error) {
	out, err := exec.CommandContext(ctx, "az", "resource", "list", "--output", "json").Output()
	if err != nil {
		return nil, fmt.Errorf("az resource list failed (are you logged in? run `az login`): %w", err)
	}
	var resources []azResource
	if err := json.Unmarshal(out, &resources); err != nil {
		return nil, fmt.Errorf("parsing az output: %w", err)
	}

	g := graph.New()
	rgSeen := map[string]bool{}

	for _, r := range resources {
		mapped, ok := typeMap[strings.ToLower(r.Type)]
		typ, group := "azure_resource", "Other"
		if ok {
			typ, group = mapped[0], mapped[1]
		}

		rgID := "rg-" + strings.ToLower(r.ResourceGroup)
		if !rgSeen[rgID] && r.ResourceGroup != "" {
			rgSeen[rgID] = true
			g.AddNode(&graph.Node{
				ID: rgID, Type: "resource_group", Provider: "azure",
				Name: r.ResourceGroup, Status: graph.StatusActive,
				Region: r.Location, Group: "Resource Groups",
			})
		}

		meta := map[string]string{"azure_type": r.Type}
		for k, v := range r.Tags {
			meta["tag:"+k] = v
		}

		g.AddNode(&graph.Node{
			ID: r.ID, Type: typ, Provider: "azure",
			Name: r.Name, Status: graph.StatusActive,
			Region: r.Location, Metadata: meta,
			Parent: rgID, Group: group,
		})
		if r.ResourceGroup != "" {
			g.AddEdge(&graph.Edge{
				ID: rgID + "->" + r.ID, Source: rgID, Target: r.ID, Type: graph.EdgeContains,
			})
		}
	}
	return g, nil
}
