package components

import (
	"reflect"
	"testing"

	"graphkit/graph"
)

func TestConnected(t *testing.T) {
	g := graph.New()
	g.AddEdge("a", "b", 1) // a-b linked (one direction is enough, undirected)
	g.AddEdge("b", "a", 1)
	g.AddEdge("c", "d", 1)
	g.AddEdge("f", "e", 1) // links e and f undirected
	g.AddNode("solo")

	want := [][]string{{"a", "b"}, {"c", "d"}, {"e", "f"}, {"solo"}}
	if got := Connected(g); !reflect.DeepEqual(got, want) {
		t.Errorf("Connected = %v, want %v", got, want)
	}
}

func TestSCC(t *testing.T) {
	g := graph.New()
	// {a,b,c} cycle, {d,e} cycle, f a sink reached from the first cycle.
	g.AddEdge("a", "b", 1)
	g.AddEdge("b", "c", 1)
	g.AddEdge("c", "a", 1)
	g.AddEdge("c", "d", 1)
	g.AddEdge("d", "e", 1)
	g.AddEdge("e", "d", 1)
	g.AddEdge("c", "f", 1)

	want := [][]string{{"a", "b", "c"}, {"d", "e"}, {"f"}}
	if got := SCC(g); !reflect.DeepEqual(got, want) {
		t.Errorf("SCC = %v, want %v", got, want)
	}
}

func TestIsStronglyConnected(t *testing.T) {
	cyc := graph.New()
	cyc.AddEdge("a", "b", 1)
	cyc.AddEdge("b", "c", 1)
	cyc.AddEdge("c", "a", 1)
	if !IsStronglyConnected(cyc) {
		t.Error("IsStronglyConnected(a->b->c->a) = false, want true")
	}

	line := graph.New()
	line.AddEdge("a", "b", 1) // no way back from b
	if IsStronglyConnected(line) {
		t.Error("IsStronglyConnected(a->b) = true, want false")
	}

	if !IsStronglyConnected(graph.New()) {
		t.Error("IsStronglyConnected(empty) = false, want true")
	}
}
