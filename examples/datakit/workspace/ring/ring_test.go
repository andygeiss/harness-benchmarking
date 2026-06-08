package ring

import (
	"reflect"
	"testing"
)

func TestNewEmpty(t *testing.T) {
	r := New(3)
	if got := r.Cap(); got != 3 {
		t.Fatalf("Cap() = %d, want 3", got)
	}
	if got := r.Len(); got != 0 {
		t.Fatalf("Len() = %d, want 0", got)
	}
	if got := r.Items(); len(got) != 0 {
		t.Fatalf("Items() = %v, want empty", got)
	}
}

func TestPushSequence(t *testing.T) {
	tests := []struct {
		name    string
		cap     int
		pushes  []int
		want    []int
		wantLen int
	}{
		{"partial", 3, []int{1, 2}, []int{1, 2}, 2},
		{"exactly full", 3, []int{1, 2, 3}, []int{1, 2, 3}, 3},
		{"overwrite oldest", 3, []int{1, 2, 3, 4, 5}, []int{3, 4, 5}, 3},
		{"single capacity", 1, []int{7, 8, 9}, []int{9}, 1},
		{"single one push", 1, []int{42}, []int{42}, 1},
		{"wrap many", 2, []int{1, 2, 3, 4, 5, 6}, []int{5, 6}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := New(tt.cap)
			for _, v := range tt.pushes {
				r.Push(v)
			}
			if got := r.Cap(); got != tt.cap {
				t.Errorf("Cap() = %d, want %d", got, tt.cap)
			}
			if got := r.Len(); got != tt.wantLen {
				t.Errorf("Len() = %d, want %d", got, tt.wantLen)
			}
			if got := r.Items(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Items() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestItemsOrderOldestToNewest(t *testing.T) {
	r := New(3)
	for _, v := range []int{10, 20, 30, 40} {
		r.Push(v)
	}
	// 10 dropped; oldest->newest is 20,30,40.
	if got, want := r.Items(), []int{20, 30, 40}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Items() = %v, want %v", got, want)
	}
}

func TestItemsReturnsCopy(t *testing.T) {
	r := New(3)
	r.Push(1)
	r.Push(2)
	got := r.Items()
	got[0] = 99
	if again := r.Items(); !reflect.DeepEqual(again, []int{1, 2}) {
		t.Fatalf("mutating Items() result changed ring: %v", again)
	}
}

func TestLenNeverExceedsCap(t *testing.T) {
	r := New(4)
	for i := 0; i < 100; i++ {
		r.Push(i)
		if r.Len() > r.Cap() {
			t.Fatalf("Len() = %d exceeded Cap() = %d", r.Len(), r.Cap())
		}
	}
	if got, want := r.Items(), []int{96, 97, 98, 99}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Items() = %v, want %v", got, want)
	}
}
