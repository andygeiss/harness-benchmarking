package queue

import "testing"

func TestNewEmpty(t *testing.T) {
	q := New()
	if got := q.Len(); got != 0 {
		t.Fatalf("Len() = %d, want 0", got)
	}
	if v, ok := q.Dequeue(); v != 0 || ok {
		t.Fatalf("Dequeue() empty = (%d, %v), want (0, false)", v, ok)
	}
	if v, ok := q.Peek(); v != 0 || ok {
		t.Fatalf("Peek() empty = (%d, %v), want (0, false)", v, ok)
	}
}

func TestFIFOOrder(t *testing.T) {
	q := New()
	in := []int{10, 20, 30}
	for _, v := range in {
		q.Enqueue(v)
	}
	if got := q.Len(); got != len(in) {
		t.Fatalf("Len() = %d, want %d", got, len(in))
	}
	if v, ok := q.Peek(); v != 10 || !ok {
		t.Fatalf("Peek() = (%d, %v), want (10, true)", v, ok)
	}
	for i, want := range in {
		v, ok := q.Dequeue()
		if v != want || !ok {
			t.Fatalf("Dequeue() #%d = (%d, %v), want (%d, true)", i, v, ok, want)
		}
		if got := q.Len(); got != len(in)-i-1 {
			t.Fatalf("after Dequeue #%d Len() = %d, want %d", i, got, len(in)-i-1)
		}
	}
	if v, ok := q.Dequeue(); v != 0 || ok {
		t.Fatalf("Dequeue() drained = (%d, %v), want (0, false)", v, ok)
	}
}

func TestInterleaved(t *testing.T) {
	q := New()
	q.Enqueue(1)
	q.Enqueue(2)
	q.Enqueue(3)
	if v, ok := q.Dequeue(); v != 1 || !ok {
		t.Fatalf("Dequeue() = (%d, %v), want (1, true)", v, ok)
	}
	q.Enqueue(4)
	want := []int{2, 3, 4}
	for i, w := range want {
		v, ok := q.Dequeue()
		if v != w || !ok {
			t.Fatalf("Dequeue() #%d = (%d, %v), want (%d, true)", i, v, ok, w)
		}
	}
	if got := q.Len(); got != 0 {
		t.Fatalf("Len() = %d, want 0", got)
	}
	if v, ok := q.Peek(); v != 0 || ok {
		t.Fatalf("Peek() drained = (%d, %v), want (0, false)", v, ok)
	}
}

func TestDuplicatesAndReuse(t *testing.T) {
	q := New()
	for _, v := range []int{5, 5, 5} {
		q.Enqueue(v)
	}
	for i := 0; i < 3; i++ {
		if v, ok := q.Dequeue(); v != 5 || !ok {
			t.Fatalf("Dequeue() dup #%d = (%d, %v), want (5, true)", i, v, ok)
		}
	}
	// Reuse after draining.
	q.Enqueue(7)
	if v, ok := q.Peek(); v != 7 || !ok {
		t.Fatalf("Peek() after reuse = (%d, %v), want (7, true)", v, ok)
	}
	if v, ok := q.Dequeue(); v != 7 || !ok {
		t.Fatalf("Dequeue() after reuse = (%d, %v), want (7, true)", v, ok)
	}
	if got := q.Len(); got != 0 {
		t.Fatalf("Len() = %d, want 0", got)
	}
}
