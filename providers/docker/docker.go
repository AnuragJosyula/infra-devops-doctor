// Package docker provides a real infrastructure provider that discovers
// containers, images, networks, and volumes from the local Docker daemon.
package docker

import (
	"context"
	"fmt"
	"strings"

	"github.com/moby/moby/client"

	"github.com/inframap/inframap/internal/graph"
	"github.com/inframap/inframap/internal/provider"
)

// ensure Docker implements Provider
var _ provider.Provider = (*Docker)(nil)

// Docker discovers infrastructure from the local Docker daemon.
type Docker struct {
	cli *client.Client
}

// New creates a new Docker provider. It connects to the local Docker daemon.
func New() (*Docker, error) {
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}
	return &Docker{cli: cli}, nil
}

func (d *Docker) Name() string        { return "docker" }
func (d *Docker) Description() string { return "Local Docker daemon (containers, images, networks, volumes)" }

// Watch is not implemented yet for the new SDK.
func (d *Docker) Watch(_ context.Context, _ chan<- graph.Event) error {
	return fmt.Errorf("docker watch not yet implemented")
}

// Discover scans the local Docker daemon and returns a graph of all resources.
func (d *Docker) Discover(ctx context.Context) (*graph.Graph, error) {
	g := graph.New()

	// Root node for Docker
	g.AddNode(&graph.Node{
		ID: "docker-daemon", Type: "docker_daemon", Provider: "docker",
		Name: "Docker Daemon", Status: graph.StatusRunning, Group: "Docker",
	})

	// Discover containers
	if err := d.discoverContainers(ctx, g); err != nil {
		return nil, fmt.Errorf("containers: %w", err)
	}

	// Discover images
	if err := d.discoverImages(ctx, g); err != nil {
		return nil, fmt.Errorf("images: %w", err)
	}

	// Discover networks
	if err := d.discoverNetworks(ctx, g); err != nil {
		return nil, fmt.Errorf("networks: %w", err)
	}

	// Discover volumes
	if err := d.discoverVolumes(ctx, g); err != nil {
		return nil, fmt.Errorf("volumes: %w", err)
	}

	return g, nil
}

func (d *Docker) discoverContainers(ctx context.Context, g *graph.Graph) error {
	result, err := d.cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return err
	}

	for _, c := range result.Items {
		name := "unnamed"
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		shortID := c.ID[:12]
		nodeID := fmt.Sprintf("docker-container-%s", shortID)

		status := graph.StatusStopped
		if c.State == "running" {
			status = graph.StatusRunning
		}

		meta := map[string]string{
			"image":    c.Image,
			"state":    string(c.State),
			"status":   c.Status,
			"short_id": shortID,
		}

		// Add port mappings to metadata
		var ports []string
		for _, p := range c.Ports {
			if p.PublicPort > 0 {
				ports = append(ports, fmt.Sprintf("%d→%d/%s", p.PublicPort, p.PrivatePort, p.Type))
			} else {
				ports = append(ports, fmt.Sprintf("%d/%s", p.PrivatePort, p.Type))
			}
		}
		if len(ports) > 0 {
			meta["ports"] = strings.Join(ports, ", ")
		}

		g.AddNode(&graph.Node{
			ID: nodeID, Type: "container", Provider: "docker",
			Name: name, Status: status, Metadata: meta,
			Parent: "docker-daemon", Group: "Containers",
		})
		g.AddEdge(&graph.Edge{
			ID: fmt.Sprintf("docker-daemon->%s", nodeID), Source: "docker-daemon",
			Target: nodeID, Type: graph.EdgeContains,
		})

		// Link container to its image
		imageID := fmt.Sprintf("docker-image-%s", sanitizeID(c.Image))
		g.AddEdge(&graph.Edge{
			ID: fmt.Sprintf("%s->%s", nodeID, imageID), Source: nodeID,
			Target: imageID, Type: graph.EdgeDependsOn, Label: "uses image",
		})

		// Link container to its networks
		if c.NetworkSettings != nil {
			for netName := range c.NetworkSettings.Networks {
				netID := fmt.Sprintf("docker-network-%s", sanitizeID(netName))
				g.AddEdge(&graph.Edge{
					ID: fmt.Sprintf("%s->%s", nodeID, netID), Source: nodeID,
					Target: netID, Type: graph.EdgeConnectsTo, Label: "attached",
				})
			}
		}
	}

	return nil
}

func (d *Docker) discoverImages(ctx context.Context, g *graph.Graph) error {
	result, err := d.cli.ImageList(ctx, client.ImageListOptions{})
	if err != nil {
		return err
	}

	for _, img := range result.Items {
		name := "<none>"
		if len(img.RepoTags) > 0 {
			name = img.RepoTags[0]
		}
		nodeID := fmt.Sprintf("docker-image-%s", sanitizeID(name))

		sizeMB := float64(img.Size) / 1024 / 1024
		meta := map[string]string{
			"size":    fmt.Sprintf("%.1f MB", sizeMB),
			"created": fmt.Sprintf("%d", img.Created),
		}
		if len(img.RepoTags) > 1 {
			meta["tags"] = strings.Join(img.RepoTags, ", ")
		}

		g.AddNode(&graph.Node{
			ID: nodeID, Type: "image", Provider: "docker",
			Name: name, Status: graph.StatusActive, Metadata: meta,
			Parent: "docker-daemon", Group: "Images",
		})
		g.AddEdge(&graph.Edge{
			ID: fmt.Sprintf("docker-daemon->%s", nodeID), Source: "docker-daemon",
			Target: nodeID, Type: graph.EdgeContains,
		})
	}

	return nil
}

func (d *Docker) discoverNetworks(ctx context.Context, g *graph.Graph) error {
	result, err := d.cli.NetworkList(ctx, client.NetworkListOptions{})
	if err != nil {
		return err
	}

	for _, net := range result.Items {
		nodeID := fmt.Sprintf("docker-network-%s", sanitizeID(net.Name))

		meta := map[string]string{
			"driver":   net.Driver,
			"scope":    net.Scope,
			"internal": fmt.Sprintf("%t", net.Internal),
		}
		if len(net.IPAM.Config) > 0 {
			meta["subnet"] = net.IPAM.Config[0].Subnet.String()
			meta["gateway"] = net.IPAM.Config[0].Gateway.String()
		}

		g.AddNode(&graph.Node{
			ID: nodeID, Type: "network", Provider: "docker",
			Name: net.Name, Status: graph.StatusActive, Metadata: meta,
			Parent: "docker-daemon", Group: "Networks",
		})
		g.AddEdge(&graph.Edge{
			ID: fmt.Sprintf("docker-daemon->%s", nodeID), Source: "docker-daemon",
			Target: nodeID, Type: graph.EdgeContains,
		})
	}

	return nil
}

func (d *Docker) discoverVolumes(ctx context.Context, g *graph.Graph) error {
	result, err := d.cli.VolumeList(ctx, client.VolumeListOptions{})
	if err != nil {
		return err
	}

	for _, vol := range result.Items {
		nodeID := fmt.Sprintf("docker-volume-%s", sanitizeID(vol.Name))

		meta := map[string]string{
			"driver":     vol.Driver,
			"mountpoint": vol.Mountpoint,
		}

		g.AddNode(&graph.Node{
			ID: nodeID, Type: "volume", Provider: "docker",
			Name: vol.Name, Status: graph.StatusActive, Metadata: meta,
			Parent: "docker-daemon", Group: "Volumes",
		})
		g.AddEdge(&graph.Edge{
			ID: fmt.Sprintf("docker-daemon->%s", nodeID), Source: "docker-daemon",
			Target: nodeID, Type: graph.EdgeContains,
		})
	}

	return nil
}

// sanitizeID creates a safe node ID from a string.
func sanitizeID(s string) string {
	r := strings.NewReplacer("/", "-", ":", "-", ".", "-", " ", "-")
	return r.Replace(strings.ToLower(s))
}
