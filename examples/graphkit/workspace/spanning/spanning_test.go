package spanning

import (
	"reflect"
	"testing"

	"graphkit/graph"
)

func TestMSTDistinctWeights(t *testing.T) {
	// Undirected edges (added one direction; MST treats them undirected):
	//   a-b 1, a-c 3, b-c 2, b-d 4, c-d 5
	// Distinct weights => the MST is unique: a-b, b-c, b-d, total 7.
	g := graph.New()
	g.AddEdge("a", "b", 1)
	g.AddEdge("a", "c", 3)
	g.AddEdge("b", "c", 2)
	g.AddEdge("b", "d", 4)
	g.AddEdge("c", "d", 5)

	tree, total, ok := MST(g)
	if !ok {
		t.Fatal("MST ok = false, want true for a connected graph")
	}
	if total != 7 {
		t.Errorf("MST total = %d, want 7", total)
	}
	want := []Edge{{"a", "b", 1}, {"b", "c", 2}, {"b", "d", 4}}
	if !reflect.DeepEqual(tree, want) {
		t.Errorf("MST tree = %v, want %v", tree, want)
	}
}

func TestMSTDisconnected(t *testing.T) {
	g := graph.New()
	g.AddEdge("a", "b", 1)
	g.AddEdge("c", "d", 2) // separate component
	if tree, total, ok := MST(g); ok || tree != nil || total != 0 {
		t.Errorf("MST(disconnected) = (%v,%d,%v), want (nil,0,false)", tree, total, ok)
	}
}

func TestMSTTrivial(t *testing.T) {
	single := graph.New()
	single.AddNode("only")
	if tree, total, ok := MST(single); !ok || total != 0 || len(tree) != 0 {
		t.Errorf("MST(single node) = (%v,%d,%v), want ([],0,true)", tree, total, ok)
	}
	if tree, total, ok := MST(graph.New()); !ok || total != 0 || len(tree) != 0 {
		t.Errorf("MST(empty) = (%v,%d,%v), want ([],0,true)", tree, total, ok)
	}
}

// TestMSTEqualWeights checks the weight and tree structure without pinning a
// specific edge set, since equal weights admit several valid minimum trees.
func TestMSTEqualWeights(t *testing.T) {
	// A unit-weight square a-b-c-d-a: any 3 of the 4 edges form an MST (weight 3).
	g := graph.New()
	g.AddEdge("a", "b", 1)
	g.AddEdge("b", "c", 1)
	g.AddEdge("c", "d", 1)
	g.AddEdge("d", "a", 1)

	tree, total, ok := MST(g)
	if !ok || total != 3 {
		t.Fatalf("MST square = (total %d, ok %v), want (3, true)", total, ok)
	}
	if len(tree) != g.Order()-1 {
		t.Fatalf("MST has %d edges, want %d (a spanning tree)", len(tree), g.Order()-1)
	}
	assertSpanningTree(t, g, tree)
}

// assertSpanningTree verifies the edges connect every node without a cycle and
// are real edges of g (in either direction).
func assertSpanningTree(t *testing.T, g *graph.Graph, tree []Edge) {
	t.Helper()
	parent := map[string]string{}
	find := func(x string) string {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	for _, n := range g.Nodes() {
		parent[n] = n
	}
	for _, e := range tree {
		if !g.HasEdge(e.U, e.V) && !g.HasEdge(e.V, e.U) {
			t.Errorf("tree edge %s-%s is not an edge of g", e.U, e.V)
		}
		ru, rv := find(e.U), find(e.V)
		if ru == rv {
			t.Errorf("tree edge %s-%s forms a cycle", e.U, e.V)
		}
		parent[ru] = rv
	}
	root := find(g.Nodes()[0])
	for _, n := range g.Nodes() {
		if find(n) != root {
			t.Errorf("node %s is not connected by the tree", n)
		}
	}
}
