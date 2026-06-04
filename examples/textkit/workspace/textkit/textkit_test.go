package textkit

import "testing"

func TestWordCount(t *testing.T) {
	cases := []struct {
		s    string
		want int
	}{
		{"", 0},
		{"   ", 0},
		{"hello", 1},
		{"hello world", 2},
		{"  a  b   c ", 3},
		{"one two three four", 4},
	}
	for _, c := range cases {
		if got := WordCount(c.s); got != c.want {
			t.Errorf("WordCount(%q) = %d, want %d", c.s, got, c.want)
		}
	}
}

func TestCountVowels(t *testing.T) {
	cases := []struct {
		s    string
		want int
	}{
		{"", 0},
		{"xyz", 0},
		{"aeiou", 5},
		{"AEIOU", 5},
		{"Hello World", 3},
		{"rhythm", 0},
	}
	for _, c := range cases {
		if got := CountVowels(c.s); got != c.want {
			t.Errorf("CountVowels(%q) = %d, want %d", c.s, got, c.want)
		}
	}
}

func TestIsPalindrome(t *testing.T) {
	yes := []string{"", "a", "abba", "racecar", "level"}
	no := []string{"ab", "abc", "racecars", "Abba"}
	for _, s := range yes {
		if !IsPalindrome(s) {
			t.Errorf("IsPalindrome(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if IsPalindrome(s) {
			t.Errorf("IsPalindrome(%q) = true, want false", s)
		}
	}
}

func TestAcronym(t *testing.T) {
	cases := []struct {
		s, want string
	}{
		{"portable network graphics", "PNG"},
		{"as soon as possible", "ASAP"},
		{"hello", "H"},
		{"", ""},
	}
	for _, c := range cases {
		if got := Acronym(c.s); got != c.want {
			t.Errorf("Acronym(%q) = %q, want %q", c.s, got, c.want)
		}
	}
}

func TestRunLengthEncode(t *testing.T) {
	cases := []struct {
		s, want string
	}{
		{"", ""},
		{"a", "1a"},
		{"aaa", "3a"},
		{"aaabbc", "3a2b1c"},
		{"wwwww", "5w"},
	}
	for _, c := range cases {
		if got := RunLengthEncode(c.s); got != c.want {
			t.Errorf("RunLengthEncode(%q) = %q, want %q", c.s, got, c.want)
		}
	}
}

func TestRunLengthDecode(t *testing.T) {
	cases := []struct {
		s, want string
	}{
		{"", ""},
		{"1a", "a"},
		{"3a", "aaa"},
		{"3a2b1c", "aaabbc"},
		{"5w", "wwwww"},
	}
	for _, c := range cases {
		got, err := RunLengthDecode(c.s)
		if err != nil {
			t.Errorf("RunLengthDecode(%q) unexpected error: %v", c.s, err)
			continue
		}
		if got != c.want {
			t.Errorf("RunLengthDecode(%q) = %q, want %q", c.s, got, c.want)
		}
	}
	for _, bad := range []string{"a", "3", "x3", "3a2"} {
		if _, err := RunLengthDecode(bad); err == nil {
			t.Errorf("RunLengthDecode(%q): expected error, got nil", bad)
		}
	}
}

func TestRotate(t *testing.T) {
	cases := []struct {
		s    string
		n    int
		want string
	}{
		{"abcde", 0, "abcde"},
		{"abcde", 2, "cdeab"},
		{"abcde", 5, "abcde"},
		{"abcde", 7, "cdeab"},
		{"abcde", -1, "eabcd"},
		{"", 3, ""},
		{"a", 9, "a"},
	}
	for _, c := range cases {
		if got := Rotate(c.s, c.n); got != c.want {
			t.Errorf("Rotate(%q, %d) = %q, want %q", c.s, c.n, got, c.want)
		}
	}
}

func TestTitle(t *testing.T) {
	cases := []struct {
		s, want string
	}{
		{"", ""},
		{"hello world", "Hello World"},
		{"the QUICK brown", "The Quick Brown"},
		{"  go  lang ", "Go Lang"},
	}
	for _, c := range cases {
		if got := Title(c.s); got != c.want {
			t.Errorf("Title(%q) = %q, want %q", c.s, got, c.want)
		}
	}
}
