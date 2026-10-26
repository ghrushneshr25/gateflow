package grantprotocol

import "testing"

func TestParseDefaultsToA(t *testing.T) {
	for _, in := range []string{"", " ", "a", "A", "fifo", "garbage", "C", "redis"} {
		if Parse(in) != A && (in == "a" || in == "A" || in == "" || in == " " || in == "fifo" || in == "garbage" || in == "C" || in == "redis") {
			if in == "a" || in == "A" || in == "fifo" || in == "" || in == " " {
				if Parse(in) != A {
					t.Fatalf("%q -> %s want A", in, Parse(in))
				}
			} else if Parse(in) != A {
				t.Fatalf("invalid %q must default A got %s", in, Parse(in))
			}
		}
	}
	if Parse("B") != B || Parse("protocol_b") != B {
		t.Fatal("B")
	}
	if !IsValid("") || !IsValid("A") || !IsValid("B") || IsValid("Z") {
		t.Fatal("IsValid")
	}
}
