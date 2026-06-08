package heap

import (
	"sort"
	"testing"
)

func TestEmpty(t *testing.T) {
	h := New()
	if got := h.Len(); got != 0 {
		t.Fatalf("Len() on empty = %d, want 0", got)
	}
	if v, ok := h.Peek(); v != 0 || ok {
		t.Fatalf("Peek() on empty = (%d, %t), want (0, false)", v, ok)
	}
	if v, ok := h.Pop(); v != 0 || ok {
		t.Fatalf("Pop() on empty = (%d, %t), want (0, false)", v, ok)
	}
}

func TestPushPopAscending(t *testing.T) {
	cases := [][]int{
		{5, 3, 8, 1, 9, 2, 7, 4, 6, 0},
		{5, 5, 3},             // duplicates
		{-3, -1, -2, 0, 2, 1}, // negatives
		{42},                  // single
		{10, 9, 8, 7, 6, 5},   // strictly descending input
		{1, 2, 3, 4, 5},       // already ascending input
		{4, 4, 4, 4},          // all equal
	}
	for _, in := range cases {
		h := New()
		for _, v := range in {
			h.Push(v)
		}
		if got := h.Len(); got != len(in) {
			t.Fatalf("input %v: Len() = %d, want %d", in, got, len(in))
		}
		want := append([]int(nil), in...)
		sort.Ints(want)
		// Peek must equal the minimum before popping.
		if pv, ok := h.Peek(); !ok || pv != want[0] {
			t.Fatalf("input %v: Peek() = (%d, %t), want (%d, true)", in, pv, ok, want[0])
		}
		var got []int
		for h.Len() > 0 {
			v, ok := h.Pop()
			if !ok {
				t.Fatalf("input %v: Pop() returned ok=false with Len()>0", in)
			}
			got = append(got, v)
		}
		if len(got) != len(want) {
			t.Fatalf("input %v: popped %v, want %v", in, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("input %v: popped %v, want sorted %v", in, got, want)
			}
		}
		// Drained heap behaves as empty.
		if v, ok := h.Pop(); v != 0 || ok {
			t.Fatalf("input %v: Pop() after drain = (%d, %t), want (0, false)", in, v, ok)
		}
	}
}

func TestPeekDoesNotRemove(t *testing.T) {
	h := New()
	for _, v := range []int{3, 1, 2} {
		h.Push(v)
	}
	for i := 0; i < 3; i++ {
		if v, ok := h.Peek(); !ok || v != 1 {
			t.Fatalf("Peek() #%d = (%d, %t), want (1, true)", i, v, ok)
		}
	}
	if got := h.Len(); got != 3 {
		t.Fatalf("Len() after repeated Peek = %d, want 3", got)
	}
}

func TestInterleaved(t *testing.T) {
	h := New()
	h.Push(5)
	h.Push(2)
	if v, ok := h.Pop(); !ok || v != 2 {
		t.Fatalf("Pop() = (%d, %t), want (2, true)", v, ok)
	}
	h.Push(1)
	h.Push(3)
	if v, ok := h.Peek(); !ok || v != 1 {
		t.Fatalf("Peek() = (%d, %t), want (1, true)", v, ok)
	}
	want := []int{1, 3, 5}
	for i, w := range want {
		if v, ok := h.Pop(); !ok || v != w {
			t.Fatalf("Pop() #%d = (%d, %t), want (%d, true)", i, v, ok, w)
		}
	}
	if got := h.Len(); got != 0 {
		t.Fatalf("Len() = %d, want 0", got)
	}
}
