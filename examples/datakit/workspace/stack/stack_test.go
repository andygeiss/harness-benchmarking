package stack

import "testing"

func TestNewIsEmpty(t *testing.T) {
	s := New()
	if got := s.Len(); got != 0 {
		t.Fatalf("Len() on new stack = %d, want 0", got)
	}
	if v, ok := s.Pop(); v != 0 || ok {
		t.Fatalf("Pop() on empty = (%d, %t), want (0, false)", v, ok)
	}
	if v, ok := s.Peek(); v != 0 || ok {
		t.Fatalf("Peek() on empty = (%d, %t), want (0, false)", v, ok)
	}
}

func TestLIFOOrder(t *testing.T) {
	s := New()
	in := []int{1, 2, 3, 4, 5}
	for _, v := range in {
		s.Push(v)
	}
	if got := s.Len(); got != len(in) {
		t.Fatalf("Len() after pushes = %d, want %d", got, len(in))
	}
	// Pop must yield reverse (LIFO) order.
	want := []int{5, 4, 3, 2, 1}
	for i, w := range want {
		v, ok := s.Pop()
		if !ok || v != w {
			t.Fatalf("Pop() #%d = (%d, %t), want (%d, true)", i, v, ok, w)
		}
		if got, exp := s.Len(), len(in)-i-1; got != exp {
			t.Fatalf("Len() after pop #%d = %d, want %d", i, got, exp)
		}
	}
	if v, ok := s.Pop(); v != 0 || ok {
		t.Fatalf("Pop() after draining = (%d, %t), want (0, false)", v, ok)
	}
}

func TestPeekDoesNotRemove(t *testing.T) {
	s := New()
	s.Push(10)
	s.Push(20)
	for i := 0; i < 3; i++ {
		if v, ok := s.Peek(); !ok || v != 20 {
			t.Fatalf("Peek() #%d = (%d, %t), want (20, true)", i, v, ok)
		}
	}
	if got := s.Len(); got != 2 {
		t.Fatalf("Len() after peeks = %d, want 2", got)
	}
	if v, ok := s.Pop(); !ok || v != 20 {
		t.Fatalf("Pop() top = (%d, %t), want (20, true)", v, ok)
	}
	if v, ok := s.Peek(); !ok || v != 10 {
		t.Fatalf("Peek() new top = (%d, %t), want (10, true)", v, ok)
	}
}

func TestInterleavedPushPop(t *testing.T) {
	s := New()
	s.Push(1)
	s.Push(2)
	if v, ok := s.Pop(); !ok || v != 2 {
		t.Fatalf("Pop() = (%d, %t), want (2, true)", v, ok)
	}
	s.Push(3) // stack now: bottom [1, 3] top
	if v, ok := s.Peek(); !ok || v != 3 {
		t.Fatalf("Peek() = (%d, %t), want (3, true)", v, ok)
	}
	if got := s.Len(); got != 2 {
		t.Fatalf("Len() = %d, want 2", got)
	}
	want := []int{3, 1}
	for i, w := range want {
		if v, ok := s.Pop(); !ok || v != w {
			t.Fatalf("Pop() #%d = (%d, %t), want (%d, true)", i, v, ok, w)
		}
	}
	if got := s.Len(); got != 0 {
		t.Fatalf("Len() after drain = %d, want 0", got)
	}
}

func TestDuplicatesAndBoundary(t *testing.T) {
	s := New()
	s.Push(7)
	s.Push(7)
	s.Push(7)
	if got := s.Len(); got != 3 {
		t.Fatalf("Len() with duplicates = %d, want 3", got)
	}
	for i := 0; i < 3; i++ {
		if v, ok := s.Pop(); !ok || v != 7 {
			t.Fatalf("Pop() #%d = (%d, %t), want (7, true)", i, v, ok)
		}
	}
	// Boundary: empty again, then reuse.
	if v, ok := s.Peek(); v != 0 || ok {
		t.Fatalf("Peek() after drain = (%d, %t), want (0, false)", v, ok)
	}
	s.Push(-1)
	if v, ok := s.Pop(); !ok || v != -1 {
		t.Fatalf("Pop() after reuse = (%d, %t), want (-1, true)", v, ok)
	}
}
