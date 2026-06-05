package paths

import (
	"reflect"
	"testing"

	"graphkit/graph"
)

// weighted builds the fixed shortest-path fixture:
//
//	a->b 1, a->c 4, b->c 1, b->d 5, c->d 1, d->e 2, c->e 7
//	x->a 1 (so x is unreachable FROM a)
//
// The unique cheapest a..e route is a-b-c-d-e at cost 5.
func weighted() *graph.Graph {
	g := graph.New()
	g.AddEdge("a", "b", 1)
	g.AddEdge("a", "c", 4)
	g.AddEdge("b", "c", 1)
	g.AddEdge("b", "d", 5)
	g.AddEdge("c", "d", 1)
	g.AddEdge("d", "e", 2)
	g.AddEdge("c", "e", 7)
	g.AddEdge("x", "a", 1)
	return g
}

// cost sums the edge weights along path and fails if any step is not an edge.
func cost(t *testing.T, g *graph.Graph, path []string) int {
	t.Helper()
	total := 0
	for i := 0; i+1 < len(path); i++ {
		w, ok := g.Weight(path[i], path[i+1])
		if !ok {
			t.Fatalf("path %v has no edge %s->%s", path, path[i], path[i+1])
		}
		total += w
	}
	return total
}

func checkEnds(t *testing.T, path []string, start, end string) {
	t.Helper()
	if len(path) == 0 || path[0] != start || path[len(path)-1] != end {
		t.Fatalf("path %v does not run %s..%s", path, start, end)
	}
}

func TestDijkstraCostAndValidity(t *testing.T) {
	g := weighted()
	path, c, ok := Dijkstra(g, "a", "e")
	if !ok {
		t.Fatal("Dijkstra(a,e) ok = false, want true")
	}
	if c != 5 {
		t.Errorf("Dijkstra(a,e) cost = %d, want 5", c)
	}
	checkEnds(t, path, "a", "e")
	if got := cost(t, g, path); got != 5 {
		t.Errorf("returned path %v sums to %d, want 5", path, got)
	}
}

func TestDijkstraUnreachable(t *testing.T) {
	if p, c, ok := Dijkstra(weighted(), "a", "x"); ok || p != nil || c != 0 {
		t.Errorf("Dijkstra(a,x) = (%v,%d,%v), want (nil,0,false)", p, c, ok)
	}
}

func TestDijkstraSameNode(t *testing.T) {
	p, c, ok := Dijkstra(weighted(), "a", "a")
	if !ok || c != 0 || !reflect.DeepEqual(p, []string{"a"}) {
		t.Errorf("Dijkstra(a,a) = (%v,%d,%v), want ([a],0,true)", p, c, ok)
	}
}

func TestDistances(t *testing.T) {
	want := map[string]int{"a": 0, "b": 1, "c": 2, "d": 3, "e": 5}
	if got := Distances(weighted(), "a"); !reflect.DeepEqual(got, want) {
		t.Errorf("Distances(a) = %v, want %v (x is unreachable, so absent)", got, want)
	}
}

func TestUnweighted(t *testing.T) {
	g := weighted()
	// Fewest-edges a..e is the unique 2-hop a-c-e (ignoring weights).
	path, ok := Unweighted(g, "a", "e")
	if !ok {
		t.Fatal("Unweighted(a,e) ok = false, want true")
	}
	if want := []string{"a", "c", "e"}; !reflect.DeepEqual(path, want) {
		t.Errorf("Unweighted(a,e) = %v, want %v", path, want)
	}
}

func TestUnweightedUnreachableAndSame(t *testing.T) {
	g := weighted()
	if p, ok := Unweighted(g, "a", "x"); ok || p != nil {
		t.Errorf("Unweighted(a,x) = (%v,%v), want (nil,false)", p, ok)
	}
	if p, ok := Unweighted(g, "a", "a"); !ok || !reflect.DeepEqual(p, []string{"a"}) {
		t.Errorf("Unweighted(a,a) = (%v,%v), want ([a],true)", p, ok)
	}
}
