// Package graph provides the core data structures for InfraMap's infrastructure graph.
// All operations are thread-safe via sync.RWMutex.
package graph

import (
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// --- Node & Edge Types ---

// Node represents a single infrastructure resource (EC2 instance, container, S3 bucket, etc.)
type Node struct {
	ID       string            `json:"id"`
	Type     string            `json:"type"`     // e.g. "ec2", "s3_bucket", "container", "pod"
	Provider string            `json:"provider"` // e.g. "aws", "docker", "kubernetes", "mock"
	Name     string            `json:"name"`
	Status   string            `json:"status"` // "running", "stopped", "healthy", "degraded", "unknown"
	Region   string            `json:"region,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
	Tags     []string          `json:"tags,omitempty"`
	Parent   string            `json:"parent,omitempty"` // ID of parent node for hierarchy
	Group    string            `json:"group,omitempty"`  // visual grouping category
	Cost     float64           `json:"cost_monthly,omitempty"` // estimated monthly cost in USD
}

// Edge represents a relationship between two nodes.
type Edge struct {
	ID       string            `json:"id"`
	Source   string            `json:"source"`
	Target   string            `json:"target"`
	Type     string            `json:"type"` // "contains", "connects_to", "depends_on", "routes_to", "attached_to"
	Label    string            `json:"label,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Common edge types
const (
	EdgeContains   = "contains"
	EdgeConnectsTo = "connects_to"
	EdgeDependsOn  = "depends_on"
	EdgeRoutesTo   = "routes_to"
	EdgeAttachedTo = "attached_to"
	EdgeMounts     = "mounts"
)

// Common node statuses
const (
	StatusRunning  = "running"
	StatusStopped  = "stopped"
	StatusHealthy  = "healthy"
	StatusDegraded = "degraded"
	StatusUnknown  = "unknown"
	StatusActive   = "active"
)

// --- Events for live updates ---

// EventType describes what changed in the graph.
type EventType string

const (
	EventNodeAdded   EventType = "node_added"
	EventNodeRemoved EventType = "node_removed"
	EventNodeUpdated EventType = "node_updated"
	EventEdgeAdded   EventType = "edge_added"
	EventEdgeRemoved EventType = "edge_removed"
)

// Event represents a single change to the graph.
type Event struct {
	Type      EventType `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Node      *Node     `json:"node,omitempty"`
	Edge      *Edge     `json:"edge,omitempty"`
}

// --- Graph ---

// Graph is a thread-safe collection of nodes and edges representing infrastructure.
type Graph struct {
	Nodes map[string]*Node `json:"nodes"`
	Edges map[string]*Edge `json:"edges"`
	mu    sync.RWMutex
}

// New creates an empty graph.
func New() *Graph {
	return &Graph{
		Nodes: make(map[string]*Node),
		Edges: make(map[string]*Edge),
	}
}

// AddNode adds or replaces a node in the graph.
func (g *Graph) AddNode(n *Node) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.Nodes[n.ID] = n
}

// AddEdge adds or replaces an edge in the graph.
func (g *Graph) AddEdge(e *Edge) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.Edges[e.ID] = e
}

// RemoveNode removes a node and all its connected edges.
func (g *Graph) RemoveNode(id string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.Nodes, id)

	// Remove all edges connected to this node
	for eid, e := range g.Edges {
		if e.Source == id || e.Target == id {
			delete(g.Edges, eid)
		}
	}
}

// RemoveEdge removes an edge by ID.
func (g *Graph) RemoveEdge(id string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.Edges, id)
}

// GetNode returns a node by ID, or nil if not found.
func (g *Graph) GetNode(id string) *Node {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.Nodes[id]
}

// GetEdge returns an edge by ID, or nil if not found.
func (g *Graph) GetEdge(id string) *Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.Edges[id]
}

// NodeSlice returns all nodes as a slice.
func (g *Graph) NodeSlice() []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()
	nodes := make([]*Node, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		nodes = append(nodes, n)
	}
	return nodes
}

// EdgeSlice returns all edges as a slice.
func (g *Graph) EdgeSlice() []*Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	edges := make([]*Edge, 0, len(g.Edges))
	for _, e := range g.Edges {
		edges = append(edges, e)
	}
	return edges
}

// GetNodesByType returns all nodes matching the given type.
func (g *Graph) GetNodesByType(typ string) []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var result []*Node
	for _, n := range g.Nodes {
		if n.Type == typ {
			result = append(result, n)
		}
	}
	return result
}

// GetNodesByProvider returns all nodes from a specific provider.
func (g *Graph) GetNodesByProvider(provider string) []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var result []*Node
	for _, n := range g.Nodes {
		if n.Provider == provider {
			result = append(result, n)
		}
	}
	return result
}

// GetChildren returns all nodes whose Parent matches the given ID.
func (g *Graph) GetChildren(parentID string) []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var result []*Node
	for _, n := range g.Nodes {
		if n.Parent == parentID {
			result = append(result, n)
		}
	}
	return result
}

// GetEdgesFrom returns all edges originating from the given node ID.
func (g *Graph) GetEdgesFrom(nodeID string) []*Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var result []*Edge
	for _, e := range g.Edges {
		if e.Source == nodeID {
			result = append(result, e)
		}
	}
	return result
}

// GetEdgesTo returns all edges pointing to the given node ID.
func (g *Graph) GetEdgesTo(nodeID string) []*Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var result []*Edge
	for _, e := range g.Edges {
		if e.Target == nodeID {
			result = append(result, e)
		}
	}
	return result
}

// Search returns nodes whose Name, Type, or ID contain the query (case-insensitive).
func (g *Graph) Search(query string) []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()

	q := strings.ToLower(query)
	var result []*Node
	for _, n := range g.Nodes {
		if strings.Contains(strings.ToLower(n.Name), q) ||
			strings.Contains(strings.ToLower(n.Type), q) ||
			strings.Contains(strings.ToLower(n.ID), q) {
			result = append(result, n)
		}
	}
	return result
}

// Merge combines another graph into this one. Existing nodes/edges are overwritten.
func (g *Graph) Merge(other *Graph) {
	g.mu.Lock()
	defer g.mu.Unlock()
	other.mu.RLock()
	defer other.mu.RUnlock()

	for id, n := range other.Nodes {
		g.Nodes[id] = n
	}
	for id, e := range other.Edges {
		g.Edges[id] = e
	}
}

// FilterNodes returns nodes matching the given filters. Empty filter values are ignored.
func (g *Graph) FilterNodes(provider, typ, status string) []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var result []*Node
	for _, n := range g.Nodes {
		if provider != "" && n.Provider != provider {
			continue
		}
		if typ != "" && n.Type != typ {
			continue
		}
		if status != "" && n.Status != status {
			continue
		}
		result = append(result, n)
	}
	return result
}

// Stats returns summary statistics about the graph.
func (g *Graph) Stats() *GraphStats {
	g.mu.RLock()
	defer g.mu.RUnlock()

	stats := &GraphStats{
		TotalNodes:      len(g.Nodes),
		TotalEdges:      len(g.Edges),
		NodesByProvider: make(map[string]int),
		NodesByType:     make(map[string]int),
		NodesByStatus:   make(map[string]int),
	}

	for _, n := range g.Nodes {
		stats.NodesByProvider[n.Provider]++
		stats.NodesByType[n.Type]++
		if n.Status != "" {
			stats.NodesByStatus[n.Status]++
		}
		stats.TotalMonthlyCost += n.Cost
	}

	return stats
}

// GraphStats holds summary statistics.
type GraphStats struct {
	TotalNodes      int            `json:"total_nodes"`
	TotalEdges      int            `json:"total_edges"`
	NodesByProvider map[string]int `json:"nodes_by_provider"`
	NodesByType     map[string]int `json:"nodes_by_type"`
	NodesByStatus   map[string]int `json:"nodes_by_status"`
	TotalMonthlyCost float64       `json:"total_monthly_cost"`
}

// ToJSON serializes the graph for API responses.
func (g *Graph) ToJSON() ([]byte, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	export := struct {
		Nodes []*Node `json:"nodes"`
		Edges []*Edge `json:"edges"`
	}{
		Nodes: make([]*Node, 0, len(g.Nodes)),
		Edges: make([]*Edge, 0, len(g.Edges)),
	}

	for _, n := range g.Nodes {
		export.Nodes = append(export.Nodes, n)
	}
	for _, e := range g.Edges {
		export.Edges = append(export.Edges, e)
	}

	return json.MarshalIndent(export, "", "  ")
}
