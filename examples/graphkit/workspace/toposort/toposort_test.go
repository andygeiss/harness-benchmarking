package toposort

import (
	"reflect"
	"testing"

	"graphkit/graph"
)

// dag builds a fixed acyclic fixture whose unique smallest-first topological
// order is a,b,c,d,e:
//
//	a->b, a->c, b->c, b->d, c->d ; e is isolated.
func dag() *graph.Graph {
	g := graph.New()
	g.AddEdge("a", "b", 1)
	g.AddEdge("a", "c", 1)
	g.AddEdge("b", "c", 1)
	g.AddEdge("b", "d", 1)
	g.AddEdge("c", "d", 1)
	g.AddNode("e")
	return g
}

func TestSort(t *testing.T) {
	want := []string{"a", "b", "c", "d", "e"}
	got, ok := Sort(dag())
	if !ok {
		t.Fatal("Sort ok = false, want true for a DAG")
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Sort = %v, want %v (smallest ready label first)", got, want)
	}
}

func TestSortRejectsCycle(t *testing.T) {
	g := graph.New()
	g.AddEdge("a", "b", 1)
	g.AddEdge("b", "c", 1)
	g.AddEdge("c", "a", 1)
	if got, ok := Sort(g); ok || got != nil {
		t.Errorf("Sort(cycle) = (%v,%v), want (nil,false)", got, ok)
	}
}

func TestHasCycle(t *testing.T) {
	if HasCycle(dag()) {
		t.Error("HasCycle(dag) = true, want false")
	}

	cyc := graph.New()
	cyc.AddEdge("a", "b", 1)
	cyc.AddEdge("b", "a", 1)
	if !HasCycle(cyc) {
		t.Error("HasCycle(a<->b) = false, want true")
	}

	self := graph.New()
	self.AddEdge("a", "a", 1)
	if !HasCycle(self) {
		t.Error("HasCycle(self-loop) = false, want true")
	}
	if _, ok := Sort(self); ok {
		t.Error("Sort(self-loop) ok = true, want false")
	}
}
