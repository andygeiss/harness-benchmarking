# Task: a small container-data-structures library

Implement the module `datakit`: five classic in-memory container types, each in
its own package. The build currently fails because every package has tests but no
implementation; the tests (`*_test.go` in each package) are your specification.

Unlike a layered library, these five packages are **independent** — none imports
another — so you may implement them in any order. Each is a self-contained
checkpoint: implement one, get `go test ./<pkg>/...` green, record progress in
`PROGRESS.md`, and move to the next.

## The one rule that makes everything testable: determinism

Every method whose result order is observable must return a result that is a pure
function of the inputs, never dependent on map iteration order. Concretely:

- `set.Set.Items` returns labels in **ascending sorted order**.
- `heap.Heap.Pop` returns values in **ascending order** (it is a min-heap).
- `ring.Ring.Items` returns elements **oldest to newest**.

If two runs of the same call could differ, the implementation is wrong.

## Packages and contracts

### `datakit/stack` — a LIFO stack of int

```go
type Stack
func New() *Stack
func (s *Stack) Push(v int)
func (s *Stack) Pop() (int, bool)   // remove & return the top; (0, false) if empty
func (s *Stack) Peek() (int, bool)  // top without removing; (0, false) if empty
func (s *Stack) Len() int
```

### `datakit/queue` — a FIFO queue of int

```go
type Queue
func New() *Queue
func (q *Queue) Enqueue(v int)
func (q *Queue) Dequeue() (int, bool) // remove & return the front; (0, false) if empty
func (q *Queue) Peek() (int, bool)    // front without removing; (0, false) if empty
func (q *Queue) Len() int
```

### `datakit/set` — a set of string

```go
type Set
func New() *Set
func (s *Set) Add(v string)
func (s *Set) Remove(v string)              // no-op if absent
func (s *Set) Contains(v string) bool
func (s *Set) Len() int
func (s *Set) Items() []string              // ascending sorted; length-0 slice when empty
func (s *Set) Union(other *Set) *Set        // a NEW set; leaves both operands unchanged
func (s *Set) Intersect(other *Set) *Set    // a NEW set; leaves both operands unchanged
```

### `datakit/heap` — a min-heap of int

```go
type Heap
func New() *Heap
func (h *Heap) Push(v int)
func (h *Heap) Pop() (int, bool)  // remove & return the MINIMUM; (0, false) if empty
func (h *Heap) Peek() (int, bool) // minimum without removing; (0, false) if empty
func (h *Heap) Len() int
```

Duplicates are allowed; popping a heap empties it in ascending order. Any correct
algorithm is fine (`container/heap`, or a hand-written binary heap).

### `datakit/ring` — a fixed-capacity ring buffer of int

```go
type Ring
func New(capacity int) *Ring  // capacity >= 1
func (r *Ring) Push(v int)    // append; when full, overwrite the OLDEST element
func (r *Ring) Len() int      // current count, never exceeds Cap
func (r *Ring) Cap() int
func (r *Ring) Items() []int  // oldest to newest
```

## Working approach

Your context resets between passes; read `PROGRESS.md` first and keep it current.
The packages are independent, so any order works — each is a checkpoint. Run
`go test ./<pkg>/...` after implementing a package to confirm it; `go test ./...`
is the final gate.
