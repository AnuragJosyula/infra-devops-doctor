// Package planpreview parses `terraform show -json <plan>` output, matches the
// planned changes against the live discovered graph, and computes the blast
// radius of destructive changes — i.e. what else breaks if this apply runs.
package planpreview

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/inframap/inframap/internal/graph"
)

// ─── terraform plan JSON (the subset we need) ───────────

type tfPlan struct {
	ResourceChanges []struct {
		Address string `json:"address"`
		Type    string `json:"type"`
		Name    string `json:"name"`
		Change  struct {
			Actions []string        `json:"actions"`
			Before  json.RawMessage `json:"before"`
			After   json.RawMessage `json:"after"`
		} `json:"change"`
	} `json:"resource_changes"`
}

// ─── result types ───────────────────────────────────────

// ChangeImpact is one planned change matched against the live graph.
type ChangeImpact struct {
	Address  string   `json:"address"`            // aws_db_instance.main
	Action   string   `json:"action"`             // create | update | delete | replace
	NodeID   string   `json:"node_id,omitempty"`  // matched live node ("" if not found)
	NodeName string   `json:"node_name,omitempty"`
	Impacted []string `json:"impacted"`           // downstream node IDs that break
	Risk     string   `json:"risk"`               // low | medium | high | critical
	Note     string   `json:"note,omitempty"`
}

// Result is the full preview response.
type Result struct {
	Changes      []ChangeImpact `json:"changes"`
	TotalDirect  int            `json:"total_direct"`
	TotalImpact  int            `json:"total_impacted"` // unique downstream nodes
	Unmatched    int            `json:"unmatched"`
	RiskiestAddr string         `json:"riskiest,omitempty"`
}

// Preview parses plan JSON and evaluates it against the graph.
func Preview(planJSON []byte, g *graph.Graph) (*Result, error) {
	var plan tfPlan
	if err := json.Unmarshal(planJSON, &plan); err != nil {
		return nil, fmt.Errorf("not valid terraform plan JSON (use `terraform show -json plan.out`): %w", err)
	}
	if len(plan.ResourceChanges) == 0 {
		return nil, fmt.Errorf("plan contains no resource changes")
	}

	adj := buildImpactAdjacency(g)
	res := &Result{Changes: []ChangeImpact{}}
	allImpacted := map[string]bool{}
	worstImpact := -1

	for _, rc := range plan.ResourceChanges {
		action := normalizeAction(rc.Change.Actions)
		if action == "no-op" || action == "read" {
			continue
		}

		ci := ChangeImpact{Address: rc.Address, Action: action, Impacted: []string{}}

		// creations don't exist in the live graph yet
		if action == "create" {
			ci.Risk = "low"
			ci.Note = "new resource — no existing dependencies"
			res.Changes = append(res.Changes, ci)
			res.TotalDirect++
			continue
		}

		node := matchNode(g, rc.Change.Before, rc.Name)
		if node == nil {
			ci.Risk = "medium"
			ci.Note = "not found in live graph — created outside this scan, or drifted"
			res.Unmatched++
			res.Changes = append(res.Changes, ci)
			res.TotalDirect++
			continue
		}
		ci.NodeID = node.ID
		ci.NodeName = node.Name

		// destructive changes get a blast radius; updates flag direct neighbors only
		if action == "delete" || action == "replace" {
			ci.Impacted = blast(adj, node.ID)
		} else if action == "update" {
			ci.Impacted = adj[node.ID] // one hop
		}
		for _, id := range ci.Impacted {
			allImpacted[id] = true
		}

		ci.Risk = riskFor(action, len(ci.Impacted))
		if len(ci.Impacted) > worstImpact && (action == "delete" || action == "replace") {
			worstImpact = len(ci.Impacted)
			res.RiskiestAddr = rc.Address
		}
		res.Changes = append(res.Changes, ci)
		res.TotalDirect++
	}

	res.TotalImpact = len(allImpacted)
	return res, nil
}

// ─── matching ───────────────────────────────────────────

// keys inside a plan's "before" object that tend to hold the live resource ID/name
var idKeys = []string{"id", "bucket", "db_instance_identifier", "cluster_identifier", "name", "function_name"}

func matchNode(g *graph.Graph, before json.RawMessage, tfName string) *graph.Node {
	var vals map[string]any
	_ = json.Unmarshal(before, &vals)

	// 1) direct ID match against graph node IDs
	for _, k := range idKeys {
		if v, ok := vals[k].(string); ok && v != "" {
			if n := g.GetNode(v); n != nil {
				return n
			}
		}
	}
	// 2) match by name (graph Name vs tag:Name / name / terraform resource name)
	candidates := []string{tfName}
	if v, ok := vals["tags"].(map[string]any); ok {
		if nm, ok := v["Name"].(string); ok {
			candidates = append(candidates, nm)
		}
	}
	for _, k := range idKeys {
		if v, ok := vals[k].(string); ok && v != "" {
			candidates = append(candidates, v)
		}
	}
	for _, n := range g.NodeSlice() {
		for _, c := range candidates {
			if c != "" && strings.EqualFold(n.Name, c) {
				return n
			}
		}
	}
	return nil
}

// ─── blast radius (same semantics as the UI) ────────────

// containment/attachment flows parent→child; dependency edges flow in reverse
// (whoever connects to a dead resource breaks).
func buildImpactAdjacency(g *graph.Graph) map[string][]string {
	adj := map[string][]string{}
	for _, e := range g.EdgeSlice() {
		if e.Type == graph.EdgeContains || e.Type == graph.EdgeAttachedTo {
			adj[e.Source] = append(adj[e.Source], e.Target)
		} else {
			adj[e.Target] = append(adj[e.Target], e.Source)
		}
	}
	return adj
}

func blast(adj map[string][]string, origin string) []string {
	seen := map[string]bool{origin: true}
	var out []string
	frontier := []string{origin}
	for len(frontier) > 0 {
		var next []string
		for _, id := range frontier {
			for _, t := range adj[id] {
				if !seen[t] {
					seen[t] = true
					out = append(out, t)
					next = append(next, t)
				}
			}
		}
		frontier = next
	}
	if out == nil {
		out = []string{}
	}
	return out
}

// ─── helpers ────────────────────────────────────────────

func normalizeAction(actions []string) string {
	a := strings.Join(actions, ",")
	switch {
	case a == "delete,create" || a == "create,delete":
		return "replace"
	case strings.Contains(a, "delete"):
		return "delete"
	case strings.Contains(a, "create"):
		return "create"
	case strings.Contains(a, "update"):
		return "update"
	case strings.Contains(a, "read"):
		return "read"
	default:
		return "no-op"
	}
}

func riskFor(action string, impacted int) string {
	switch {
	case (action == "delete" || action == "replace") && impacted >= 5:
		return "critical"
	case (action == "delete" || action == "replace") && impacted >= 1:
		return "high"
	case action == "delete" || action == "replace":
		return "medium"
	case action == "update" && impacted >= 1:
		return "medium"
	default:
		return "low"
	}
}
