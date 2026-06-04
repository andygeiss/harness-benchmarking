package calc

import (
	"math"
	"testing"
)

func TestEvalValid(t *testing.T) {
	cases := []struct {
		expr string
		want float64
	}{
		{"1", 1},
		{"1 + 2", 3},
		{"2 * 3 + 4", 10},
		{"2 + 3 * 4", 14},
		{"(2 + 3) * 4", 20},
		{"10 - 2 - 3", 5},
		{"20 / 2 / 5", 2},
		{"-5 + 3", -2},
		{"-(3 + 4)", -7},
		{"2 * -3", -6},
		{"3.5 * 2", 7},
		{"1 / 4", 0.25},
		{"  7  ", 7},
		{"((1))", 1},
		{"2 * (3 + (4 - 1))", 12},
	}
	for _, c := range cases {
		got, err := Eval(c.expr)
		if err != nil {
			t.Errorf("Eval(%q) unexpected error: %v", c.expr, err)
			continue
		}
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("Eval(%q) = %v, want %v", c.expr, got, c.want)
		}
	}
}

func TestEvalErrors(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"1 +",
		"* 2",
		"(1 + 2",
		"1 + 2)",
		"1 2",
		"1 / 0",
		"3 +/ 4",
		"abc",
		"2 ** 3",
	}
	for _, expr := range cases {
		if _, err := Eval(expr); err == nil {
			t.Errorf("Eval(%q): expected error, got nil", expr)
		}
	}
}
