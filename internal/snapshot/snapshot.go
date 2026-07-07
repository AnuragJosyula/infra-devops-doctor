// Package snapshot persists point-in-time copies of the graph to local JSON
// files and computes diffs between a snapshot and the current graph.
package snapshot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"time"

	"github.com/inframap/inframap/internal/graph"
)

const dir = "snapshots"
const keep = 30

type file struct {
	Time  time.Time     `json:"time"`
	Nodes []*graph.Node `json:"nodes"`
	Edges []*graph.Edge `json:"edges"`
}

// Meta describes one stored snapshot.
type Meta struct {
	File  string    `json:"file"`
	Time  time.Time `json:"time"`
	Nodes int       `json:"nodes"`
	Edges int       `json:"edges"`
}

// Diff is the result of comparing a snapshot (old) to the current graph (new).
type Diff struct {
	Added   []*graph.Node `json:"added"`   // in current, not in snapshot
	Removed []*graph.Node `json:"removed"` // in snapshot, not in current
	Changed []Change      `json:"changed"`
}

// Change is a node present in both with differing fields.
type Change struct {
	Node   *graph.Node `json:"node"`
	Fields []string    `json:"fields"`
}

// Save writes the graph to snapshots/<timestamp>.json and prunes old files.
func Save(g *graph.Graph) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	f := file{Time: time.Now(), Nodes: g.NodeSlice(), Edges: g.EdgeSlice()}
	data, err := json.Marshal(f)
	if err != nil {
		return err
	}
	name := filepath.Join(dir, f.Time.Format("2006-01-02T15-04-05")+".json")
	if err := os.WriteFile(name, data, 0644); err != nil {
		return err
	}
	prune()
	return nil
}

// List returns metadata for all snapshots, newest first.
func List() ([]Meta, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []Meta{}, nil
	}
	if err != nil {
		return nil, err
	}
	metas := []Meta{}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		f, err := load(e.Name())
		if err != nil {
			continue
		}
		metas = append(metas, Meta{File: e.Name(), Time: f.Time, Nodes: len(f.Nodes), Edges: len(f.Edges)})
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].Time.After(metas[j].Time) })
	return metas, nil
}

// DiffCurrent compares the named snapshot against the current graph.
func DiffCurrent(g *graph.Graph, name string) (*Diff, error) {
	old, err := load(name)
	if err != nil {
		return nil, err
	}

	oldByID := map[string]*graph.Node{}
	for _, n := range old.Nodes {
		oldByID[n.ID] = n
	}

	d := &Diff{Added: []*graph.Node{}, Removed: []*graph.Node{}, Changed: []Change{}}
	seen := map[string]bool{}

	for _, cur := range g.NodeSlice() {
		seen[cur.ID] = true
		prev, ok := oldByID[cur.ID]
		if !ok {
			d.Added = append(d.Added, cur)
			continue
		}
		var fields []string
		if cur.Status != prev.Status {
			fields = append(fields, fmt.Sprintf("status: %s → %s", prev.Status, cur.Status))
		}
		if cur.Name != prev.Name {
			fields = append(fields, fmt.Sprintf("name: %s → %s", prev.Name, cur.Name))
		}
		if !reflect.DeepEqual(cur.Metadata, prev.Metadata) {
			fields = append(fields, "metadata changed")
		}
		if len(fields) > 0 {
			d.Changed = append(d.Changed, Change{Node: cur, Fields: fields})
		}
	}
	for id, prev := range oldByID {
		if !seen[id] {
			d.Removed = append(d.Removed, prev)
		}
	}
	return d, nil
}

func load(name string) (*file, error) {
	// prevent path traversal
	data, err := os.ReadFile(filepath.Join(dir, filepath.Base(name)))
	if err != nil {
		return nil, err
	}
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

func prune() {
	metas, err := List()
	if err != nil {
		return
	}
	for i := keep; i < len(metas); i++ {
		os.Remove(filepath.Join(dir, metas[i].File))
	}
}
