// Package export provides graph serialization to various output formats.
package export

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/inframap/inframap/internal/graph"
)

// Format represents a supported export format.
type Format string

const (
	FormatJSON    Format = "json"
	FormatDOT     Format = "dot"
	FormatMermaid Format = "mermaid"
)

// Export converts a graph to the specified format.
func Export(g *graph.Graph, format Format) (string, error) {
	switch format {
	case FormatJSON:
		return exportJSON(g)
	case FormatDOT:
		return exportDOT(g)
	case FormatMermaid:
		return exportMermaid(g)
	case FormatTerraform:
		return exportTerraform(g)
	default:
		return "", fmt.Errorf("unsupported format: %s", format)
	}
}

// exportJSON returns the graph as formatted JSON.
func exportJSON(g *graph.Graph) (string, error) {
	data, err := json.MarshalIndent(struct {
		Nodes []*graph.Node `json:"nodes"`
		Edges []*graph.Edge `json:"edges"`
		Stats *graph.GraphStats `json:"stats"`
	}{
		Nodes: g.NodeSlice(),
		Edges: g.EdgeSlice(),
		Stats: g.Stats(),
	}, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// exportDOT returns the graph in Graphviz DOT format.
func exportDOT(g *graph.Graph) (string, error) {
	var b strings.Builder

	b.WriteString("digraph InfraMap {\n")
	b.WriteString("  rankdir=TB;\n")
	b.WriteString("  node [shape=box, style=\"rounded,filled\", fontname=\"Arial\"];\n")
	b.WriteString("  edge [fontname=\"Arial\", fontsize=10];\n\n")

	// Color mapping by provider
	providerColors := map[string]string{
		"aws":        "#FF9900",
		"docker":     "#2496ED",
		"kubernetes": "#326CE5",
		"terraform":  "#7B42BC",
		"mock":       "#FF9900",
	}

	// Write nodes
	for _, n := range g.NodeSlice() {
		color := providerColors[n.Provider]
		if color == "" {
			color = "#CCCCCC"
		}
		label := fmt.Sprintf("%s\\n(%s)", n.Name, n.Type)
		b.WriteString(fmt.Sprintf("  %q [label=%q, fillcolor=%q, fontcolor=\"white\"];\n",
			n.ID, label, color))
	}

	b.WriteString("\n")

	// Write edges
	for _, e := range g.EdgeSlice() {
		style := "solid"
		if e.Type == graph.EdgeContains {
			style = "dashed"
		}
		label := e.Type
		if e.Label != "" {
			label = e.Label
		}
		b.WriteString(fmt.Sprintf("  %q -> %q [label=%q, style=%q];\n",
			e.Source, e.Target, label, style))
	}

	b.WriteString("}\n")
	return b.String(), nil
}

// exportMermaid returns the graph as a Mermaid flowchart.
func exportMermaid(g *graph.Graph) (string, error) {
	var b strings.Builder

	b.WriteString("graph TD\n")

	// Write nodes with shapes based on type
	for _, n := range g.NodeSlice() {
		sanitizedID := sanitizeMermaidID(n.ID)
		label := fmt.Sprintf("%s<br/>%s", n.Name, n.Type)

		switch {
		case n.Type == "vpc" || n.Type == "subnet" || n.Type == "region":
			b.WriteString(fmt.Sprintf("  %s[[\"%s\"]]\n", sanitizedID, label))
		case n.Type == "s3_bucket" || n.Type == "rds" || n.Type == "elasticache":
			b.WriteString(fmt.Sprintf("  %s[(\"%s\")]\n", sanitizedID, label))
		case n.Type == "lambda":
			b.WriteString(fmt.Sprintf("  %s>\" %s \"]\n", sanitizedID, label))
		default:
			b.WriteString(fmt.Sprintf("  %s[\"%s\"]\n", sanitizedID, label))
		}
	}

	b.WriteString("\n")

	// Write edges
	for _, e := range g.EdgeSlice() {
		srcID := sanitizeMermaidID(e.Source)
		tgtID := sanitizeMermaidID(e.Target)

		switch e.Type {
		case graph.EdgeContains:
			b.WriteString(fmt.Sprintf("  %s -.- %s\n", srcID, tgtID))
		case graph.EdgeRoutesTo:
			b.WriteString(fmt.Sprintf("  %s ==> %s\n", srcID, tgtID))
		default:
			b.WriteString(fmt.Sprintf("  %s --> %s\n", srcID, tgtID))
		}
	}

	return b.String(), nil
}

// sanitizeMermaidID replaces characters that Mermaid doesn't allow in IDs.
func sanitizeMermaidID(id string) string {
	r := strings.NewReplacer("-", "_", ".", "_", "/", "_", ":", "_")
	return r.Replace(id)
}
