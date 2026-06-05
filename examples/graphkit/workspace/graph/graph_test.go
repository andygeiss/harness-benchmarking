package graph

import (
	"reflect"
	"testing"
)

func TestEmptyGraph(t *testing.T) {
	g := New()
	if g.Order() != 0 {
		t.Errorf("Order() = %d, want 0", g.Order())
	}
	if g.Size() != 0 {
		t.Errorf("Size() = %d, want 0", g.Size())
	}
	if g.Nodes() != nil && len(g.Nodes()) != 0 {
		t.Errorf("Nodes() = %v, want empty", g.Nodes())
	}
	if g.HasNode("x") {
		t.Error("HasNode(x) = true, want false")
	}
}

func TestAddNodeAndEdge(t *testing.T) {
	g := New()
	g.AddNode("solo")      // isolated node
	g.AddEdge("a", "b", 5) // creates a and b
	g.AddEdge("a", "c", 7)

	if g.Order() != 4 {
		t.Errorf("Order() = %d, want 4 (a, b, c, solo)", g.Order())
	}
	if g.Size() != 2 {
		t.Errorf("Size() = %d, want 2", g.Size())
	}
	for _, id := range []string{"a", "b", "c", "solo"} {
		if !g.HasNode(id) {
			t.Errorf("HasNode(%q) = false, want true", id)
		}
	}
	if !g.HasEdge("a", "b") {
		t.Error("HasEdge(a,b) = false, want true")
	}
	if g.HasEdge("b", "a") {
		t.Error("HasEdge(b,a) = true, want false (edges are directed)")
	}
	if w, ok := g.Weight("a", "c"); !ok || w != 7 {
		t.Errorf("Weight(a,c) = (%d,%v), want (7,true)", w, ok)
	}
}

func TestAddEdgeUpdatesWeight(t *testing.T) {
	g := New()
	g.AddEdge("a", "b", 1)
	g.AddEdge("a", "b", 9) // same pair: update, not duplicate
	if g.Size() != 1 {
		t.Errorf("Size() = %d, want 1 after re-adding the same pair", g.Size())
	}
	if w, _ := g.Weight("a", "b"); w != 9 {
		t.Errorf("Weight(a,b) = %d, want 9 (updated)", w)
	}
}

func TestNodesSorted(t *testing.T) {
	g := New()
	for _, id := range []string{"delta", "alpha", "charlie", "bravo"} {
		g.AddNode(id)
	}
	want := []string{"alpha", "bravo", "charlie", "delta"}
	if got := g.Nodes(); !reflect.DeepEqual(got, want) {
		t.Errorf("Nodes() = %v, want %v (ascending)", got, want)
	}
}

func TestNeighborsSorted(t *testing.T) {
	g := New()
	g.AddEdge("a", "z", 1)
	g.AddEdge("a", "m", 2)
	g.AddEdge("a", "b", 3)
	want := []Edge{{"b", 3}, {"m", 2}, {"z", 1}}
	if got := g.Neighbors("a"); !reflect.DeepEqual(got, want) {
		t.Errorf("Neighbors(a) = %v, want %v (ascending by To)", got, want)
	}
	if g.Neighbors("z") != nil {
		t.Errorf("Neighbors(z) = %v, want nil (no out-edges)", g.Neighbors("z"))
	}
	if g.Neighbors("absent") != nil {
		t.Errorf("Neighbors(absent) = %v, want nil", g.Neighbors("absent"))
	}
}

func TestTranspose(t *testing.T) {
	g := New()
	g.AddEdge("a", "b", 1)
	g.AddEdge("a", "c", 2)
	g.AddNode("solo")

	tr := g.Transpose()
	if !tr.HasEdge("b", "a") || !tr.HasEdge("c", "a") {
		t.Error("Transpose did not reverse the edges")
	}
	if tr.HasEdge("a", "b") {
		t.Error("Transpose kept an original-direction edge")
	}
	if w, _ := tr.Weight("c", "a"); w != 2 {
		t.Errorf("Transpose Weight(c,a) = %d, want 2 (weight preserved)", w)
	}
	if !tr.HasNode("solo") {
		t.Error("Transpose dropped an isolated node")
	}
	if g.HasEdge("b", "a") {
		t.Error("Transpose mutated the receiver")
	}
}
