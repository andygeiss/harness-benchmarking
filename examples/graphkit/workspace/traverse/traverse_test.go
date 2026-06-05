package traverse

import (
	"reflect"
	"testing"

	"graphkit/graph"
)

// sample builds the fixed traversal fixture:
//
//	a -> b, a -> c, b -> d, c -> d, c -> e, d -> f, e -> f
//	z is isolated (added, no edges).
func sample() *graph.Graph {
	g := graph.New()
	g.AddEdge("a", "b", 1)
	g.AddEdge("a", "c", 1)
	g.AddEdge("b", "d", 1)
	g.AddEdge("c", "d", 1)
	g.AddEdge("c", "e", 1)
	g.AddEdge("d", "f", 1)
	g.AddEdge("e", "f", 1)
	g.AddNode("z")
	return g
}

func TestBFS(t *testing.T) {
	want := []string{"a", "b", "c", "d", "e", "f"}
	if got := BFS(sample(), "a"); !reflect.DeepEqual(got, want) {
		t.Errorf("BFS(a) = %v, want %v", got, want)
	}
}

func TestDFS(t *testing.T) {
	want := []string{"a", "b", "d", "f", "c", "e"}
	if got := DFS(sample(), "a"); !reflect.DeepEqual(got, want) {
		t.Errorf("DFS(a) = %v, want %v", got, want)
	}
}

func TestReachable(t *testing.T) {
	want := []string{"a", "b", "c", "d", "e", "f"} // not z
	if got := Reachable(sample(), "a"); !reflect.DeepEqual(got, want) {
		t.Errorf("Reachable(a) = %v, want %v", got, want)
	}
}

func TestIsolatedStart(t *testing.T) {
	g := sample()
	if got := BFS(g, "z"); !reflect.DeepEqual(got, []string{"z"}) {
		t.Errorf("BFS(z) = %v, want [z]", got)
	}
	if got := Reachable(g, "z"); !reflect.DeepEqual(got, []string{"z"}) {
		t.Errorf("Reachable(z) = %v, want [z]", got)
	}
}

func TestUnknownStart(t *testing.T) {
	g := sample()
	if got := BFS(g, "nope"); got != nil {
		t.Errorf("BFS(nope) = %v, want nil", got)
	}
	if got := DFS(g, "nope"); got != nil {
		t.Errorf("DFS(nope) = %v, want nil", got)
	}
	if got := Reachable(g, "nope"); got != nil {
		t.Errorf("Reachable(nope) = %v, want nil", got)
	}
}
