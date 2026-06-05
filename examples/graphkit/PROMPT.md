# Task: a small graph algorithms library

Implement the module `graphkit`: a directed, weighted graph plus a set of classic
algorithms over it, spread across several packages. The build currently fails
because every package has tests but no implementation; the tests
(`*_test.go` in each package) are your specification.

This is **larger than the other examples** — six packages, ~15 functions. It is
meant to take **several passes**: implement a package, run its tests, record
progress in `PROGRESS.md`, and continue. Build the `graph` package **first** —
every other package imports it.

## The one rule that makes everything testable: determinism

Go's map iteration order is random. Every function below must return a result
that is a **pure function of the input graph**, never dependent on map order.
Concretely:

- Whenever you iterate over nodes or a node's neighbors in a way that affects the
  output, do it in **ascending label order**.
- Slices of nodes/components are returned **sorted** exactly as each function
  specifies.

If two runs of the same call could differ, the implementation is wrong.

## Packages and contracts

### `graphkit/graph` — the core (implement first)

```go
type Edge struct { To string; Weight int }

type Graph
func New() *Graph
func (g *Graph) AddNode(id string)                     // isolated node; no-op if present
func (g *Graph) AddEdge(from, to string, weight int)   // directed; creates endpoints; re-adding a pair updates its weight
func (g *Graph) HasNode(id string) bool
func (g *Graph) HasEdge(from, to string) bool
func (g *Graph) Weight(from, to string) (int, bool)
func (g *Graph) Order() int                            // node count
func (g *Graph) Size() int                             // directed-edge count
func (g *Graph) Nodes() []string                       // all labels, ascending
func (g *Graph) Neighbors(id string) []Edge            // out-edges, ascending by To; nil if none or id absent
func (g *Graph) Transpose() *Graph                      // every edge reversed; isolated nodes kept; receiver unchanged
```

There is at most one edge per ordered `(from, to)` pair.

### `graphkit/traverse` — walks from a start node

```go
func BFS(g *graph.Graph, start string) []string        // breadth-first order, start first; nil if start absent
func DFS(g *graph.Graph, start string) []string        // depth-first preorder, start first; nil if start absent
func Reachable(g *graph.Graph, start string) []string  // set reachable from start (incl. start), ascending; nil if start absent
```

Explore each node's neighbors in ascending label order, so `BFS`/`DFS` are unique.

### `graphkit/paths` — shortest paths

```go
func Unweighted(g *graph.Graph, start, end string) ([]string, bool)        // fewest edges; path incl. both ends
func Dijkstra(g *graph.Graph, start, end string) ([]string, int, bool)     // min total weight; non-negative weights
func Distances(g *graph.Graph, start string) map[string]int                // min weight start->every reachable node (start=0)
```

- A returned path includes both endpoints; `start == end` (present) yields
  `([]string{start}, …)` with cost `0`.
- Unreachable target, or a missing endpoint: `Unweighted` returns `(nil, false)`;
  `Dijkstra` returns `(nil, 0, false)`.
- `Distances` omits unreachable nodes, and returns `nil` if `start` is absent.
- When several shortest paths tie, returning any one of minimum cost/length is
  accepted — but explore neighbors in ascending order so your result is stable.

### `graphkit/toposort` — ordering and cycle detection

```go
func Sort(g *graph.Graph) ([]string, bool)   // topological order; (nil,false) if g has a cycle
func HasCycle(g *graph.Graph) bool           // a self-loop counts as a cycle
```

`Sort` must be the **unique** order produced by always emitting the
**smallest-labelled** node that currently has no incoming edges (Kahn's
algorithm).

### `graphkit/components` — grouping nodes

```go
func Connected(g *graph.Graph) [][]string          // UNDIRECTED components
func SCC(g *graph.Graph) [][]string                // strongly connected components (directed)
func IsStronglyConnected(g *graph.Graph) bool      // single SCC; empty graph / single node count as true
```

`Connected` treats every directed edge `u->v` as also connecting `v` to `u`.
Both `Connected` and `SCC` return each component **sorted ascending**, with the
components ordered by their **smallest member**. Isolated nodes are singleton
components.

### `graphkit/spanning` — minimum spanning tree

```go
type Edge struct { U, V string; Weight int }
func MST(g *graph.Graph) ([]Edge, int, bool)
```

`MST` treats `g` as **undirected** (each directed edge `u->v` is the undirected
edge `{u,v}` of that weight; if both directions exist they carry equal weight).
Self-loops are ignored. It returns the tree edges — each oriented `U < V`, the
slice **sorted by `(U, V)`** — the total weight, and `true` when `g` is
connected. A disconnected graph returns `(nil, 0, false)`. The empty graph and
any single node are connected with an empty tree of weight `0`. When several
minimum trees exist (equal weights), any one is accepted; only the total weight
is unique.

## Rules

- Put each package in its own directory under the module root, matching the test
  files' package and import paths (`graphkit/graph`, `graphkit/traverse`, …).
- Do not modify any `*_test.go` file — they are the fixed specification.
- Use only the Go standard library.
- You are done when `go test ./...` passes.

## How to approach it (several passes)

Your context resets between passes; read `PROGRESS.md` first and keep it current.
A natural order, each a checkpoint to verify before moving on:

1. `graph` — the data structure everything else needs. Get `go test ./graph/...`
   green first.
2. `traverse` — BFS/DFS/Reachable.
3. `paths` — Unweighted, then Dijkstra (a `container/heap` priority queue) and
   Distances.
4. `toposort` — Kahn's algorithm and cycle detection.
5. `components` — undirected `Connected`, then `SCC` (Tarjan or Kosaraju).
6. `spanning` — `MST` (Kruskal or Prim with union-find).

Run `go test ./<pkg>/...` after each package to see how far you have gotten;
`go test ./...` is the final gate.
