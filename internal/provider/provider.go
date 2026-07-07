// Package provider defines the interface that all infrastructure providers must implement,
// and a registry for managing provider lifecycle.
package provider

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/inframap/inframap/internal/graph"
)

// Provider is the interface that every infrastructure source must implement.
// The core engine never knows about AWS, Docker, etc. — it only talks to this interface.
type Provider interface {
	// Name returns the provider's unique identifier (e.g., "aws", "docker", "mock").
	Name() string

	// Description returns a human-readable description.
	Description() string

	// Discover scans the infrastructure and returns a graph of discovered resources.
	Discover(ctx context.Context) (*graph.Graph, error)

	// Watch starts watching for live changes and sends events to the channel.
	// Returns nil if watching is not supported.
	Watch(ctx context.Context, events chan<- graph.Event) error
}

// Info holds metadata about a registered provider.
type Info struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}

// Registry manages all registered providers.
type Registry struct {
	providers map[string]Provider
	mu        sync.RWMutex
}

// NewRegistry creates an empty provider registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
	}
}

// Register adds a provider to the registry.
func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Name()] = p
}

// Get returns a provider by name, or an error if not found.
func (r *Registry) Get(name string) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider %q not found", name)
	}
	return p, nil
}

// List returns info about all registered providers.
func (r *Registry) List() []Info {
	r.mu.RLock()
	defer r.mu.RUnlock()
	infos := make([]Info, 0, len(r.providers))
	for _, p := range r.providers {
		infos = append(infos, Info{
			Name:        p.Name(),
			Description: p.Description(),
			Enabled:     true,
		})
	}
	return infos
}

// Names returns the names of all registered providers.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}

// DiscoverAll runs discovery on all registered providers (or a filtered subset)
// and returns a merged graph.
func (r *Registry) DiscoverAll(ctx context.Context, filter []string) (*graph.Graph, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	merged := graph.New()

	// Determine which providers to run
	targets := r.providers
	if len(filter) > 0 {
		targets = make(map[string]Provider)
		for _, name := range filter {
			p, ok := r.providers[name]
			if !ok {
				return nil, fmt.Errorf("provider %q not found", name)
			}
			targets[name] = p
		}
	}

	// Run each provider's discovery. A failing provider is skipped (plug-and-play:
	// whoever runs this only gets graphs for environments they actually have),
	// but if every provider fails we surface the first error.
	var firstErr error
	succeeded := 0
	for name, p := range targets {
		g, err := p.Discover(ctx)
		if err != nil {
			log.Printf("⚠️  provider %q discovery failed, skipping: %v", name, err)
			if firstErr == nil {
				firstErr = fmt.Errorf("provider %q discovery failed: %w", name, err)
			}
			continue
		}
		merged.Merge(g)
		succeeded++
	}
	if succeeded == 0 && firstErr != nil {
		return nil, firstErr
	}

	return merged, nil
}
