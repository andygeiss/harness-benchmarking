package set

import (
	"reflect"
	"testing"
)

func mk(vs ...string) *Set {
	s := New()
	for _, v := range vs {
		s.Add(v)
	}
	return s
}

func TestEmpty(t *testing.T) {
	s := New()
	if s.Len() != 0 {
		t.Fatalf("Len = %d, want 0", s.Len())
	}
	if got := s.Items(); len(got) != 0 {
		t.Fatalf("Items = %v, want length 0", got)
	}
	if s.Contains("x") {
		t.Fatal("Contains on empty set = true")
	}
}

func TestAddDuplicateAndContains(t *testing.T) {
	s := New()
	s.Add("a")
	s.Add("a")
	if s.Len() != 1 {
		t.Fatalf("Len after duplicate Add = %d, want 1", s.Len())
	}
	if !s.Contains("a") {
		t.Fatal("Contains(a) = false, want true")
	}
	if s.Contains("b") {
		t.Fatal("Contains(b) = true, want false")
	}
}

func TestRemove(t *testing.T) {
	s := mk("a", "b")
	s.Remove("a")
	if s.Contains("a") {
		t.Fatal("Contains(a) after Remove = true")
	}
	if s.Len() != 1 {
		t.Fatalf("Len = %d, want 1", s.Len())
	}
	s.Remove("absent") // no-op
	if s.Len() != 1 {
		t.Fatalf("Len after removing absent = %d, want 1", s.Len())
	}
}

func TestItemsSorted(t *testing.T) {
	s := mk("c", "a", "b", "a")
	want := []string{"a", "b", "c"}
	if got := s.Items(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Items = %v, want %v", got, want)
	}
}

func TestUnion(t *testing.T) {
	a := mk("a", "b")
	b := mk("b", "c")
	u := a.Union(b)
	if got, want := u.Items(), []string{"a", "b", "c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Union Items = %v, want %v", got, want)
	}
	// operands unchanged
	if got, want := a.Items(), []string{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("receiver mutated: %v, want %v", got, want)
	}
	if got, want := b.Items(), []string{"b", "c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("other mutated: %v, want %v", got, want)
	}
	// result is independent
	u.Add("z")
	if a.Contains("z") || b.Contains("z") {
		t.Fatal("mutating union result leaked into operands")
	}
}

func TestIntersect(t *testing.T) {
	a := mk("a", "b", "c")
	b := mk("b", "c", "d")
	i := a.Intersect(b)
	if got, want := i.Items(), []string{"b", "c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Intersect Items = %v, want %v", got, want)
	}
	// operands unchanged
	if got, want := a.Items(), []string{"a", "b", "c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("receiver mutated: %v, want %v", got, want)
	}
	if got, want := b.Items(), []string{"b", "c", "d"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("other mutated: %v, want %v", got, want)
	}
}

func TestIntersectDisjoint(t *testing.T) {
	a := mk("a", "b")
	b := mk("c", "d")
	i := a.Intersect(b)
	if i.Len() != 0 {
		t.Fatalf("disjoint Intersect Len = %d, want 0", i.Len())
	}
	if got := i.Items(); len(got) != 0 {
		t.Fatalf("disjoint Intersect Items = %v, want length 0", got)
	}
}
