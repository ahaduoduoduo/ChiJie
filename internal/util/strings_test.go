package util

import (
	"reflect"
	"testing"
)

func TestFirstNonEmpty(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"", "  ", "a"}, "a"},
		{[]string{"", "b", "c"}, "b"},
		{[]string{"", " ", "\t"}, ""},
		{nil, ""},
	}
	for _, c := range cases {
		if got := FirstNonEmpty(c.in...); got != c.want {
			t.Errorf("FirstNonEmpty(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestContainsString(t *testing.T) {
	if !ContainsString([]string{"a", "b"}, "b") {
		t.Errorf("expected true")
	}
	if ContainsString([]string{"a", "b"}, "c") {
		t.Errorf("expected false")
	}
	if ContainsString(nil, "x") {
		t.Errorf("nil slice should return false")
	}
}

func TestRemoveString(t *testing.T) {
	got := RemoveString([]string{"a", "b", "a", "c"}, "a")
	if !reflect.DeepEqual(got, []string{"b", "c"}) {
		t.Errorf("got %v", got)
	}
}

func TestParseInt(t *testing.T) {
	if ParseInt("42") != 42 {
		t.Errorf("ParseInt(42) failed")
	}
	if ParseInt("") != 0 {
		t.Errorf("empty string should be 0")
	}
	if ParseInt("not-a-number") != 0 {
		t.Errorf("invalid input should be 0")
	}
}

func TestSplitList(t *testing.T) {
	got := SplitList("a, b\nc|d| , ,e")
	want := []string{"a", "b", "c", "d", "e"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
	if got := SplitList(""); len(got) != 0 {
		t.Errorf("empty input should return empty/nil")
	}
}
