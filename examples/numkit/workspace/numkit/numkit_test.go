package numkit

import "testing"

func TestGCD(t *testing.T) {
	cases := []struct{ a, b, want int }{
		{12, 18, 6},
		{18, 12, 6},
		{7, 1, 1},
		{0, 5, 5},
		{5, 0, 5},
		{17, 13, 1},
		{100, 80, 20},
		{0, 0, 0},
	}
	for _, c := range cases {
		if got := GCD(c.a, c.b); got != c.want {
			t.Errorf("GCD(%d, %d) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestIsPrime(t *testing.T) {
	primes := []int{2, 3, 5, 7, 11, 13, 97, 7919}
	composites := []int{-1, 0, 1, 4, 6, 9, 100, 1000}
	for _, n := range primes {
		if !IsPrime(n) {
			t.Errorf("IsPrime(%d) = false, want true", n)
		}
	}
	for _, n := range composites {
		if IsPrime(n) {
			t.Errorf("IsPrime(%d) = true, want false", n)
		}
	}
}

func TestDigitSum(t *testing.T) {
	cases := []struct{ n, want int }{
		{0, 0},
		{5, 5},
		{123, 6},
		{99, 18},
		{-45, 9},
		{1000000, 1},
	}
	for _, c := range cases {
		if got := DigitSum(c.n); got != c.want {
			t.Errorf("DigitSum(%d) = %d, want %d", c.n, got, c.want)
		}
	}
}

func TestFactorial(t *testing.T) {
	cases := []struct{ n, want int }{
		{0, 1},
		{1, 1},
		{5, 120},
		{7, 5040},
		{10, 3628800},
	}
	for _, c := range cases {
		if got := Factorial(c.n); got != c.want {
			t.Errorf("Factorial(%d) = %d, want %d", c.n, got, c.want)
		}
	}
}

func TestFib(t *testing.T) {
	cases := []struct{ n, want int }{
		{0, 0},
		{1, 1},
		{2, 1},
		{3, 2},
		{10, 55},
		{20, 6765},
	}
	for _, c := range cases {
		if got := Fib(c.n); got != c.want {
			t.Errorf("Fib(%d) = %d, want %d", c.n, got, c.want)
		}
	}
}
