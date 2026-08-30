package sysinfo

import "testing"

func TestParseLoadFields(t *testing.T) {
	l := parseLoadFields([]string{"1.50", "2.25", "3.00", "1/512", "9"})
	if !l.OK || l.One != 1.5 || l.Five != 2.25 || l.Fifteen != 3 {
		t.Fatalf("parseLoadFields = %+v", l)
	}
	if bad := parseLoadFields([]string{"1.0"}); bad.OK {
		t.Fatal("short input should not report OK")
	}
	if bad := parseLoadFields([]string{"a", "b", "c"}); bad.OK {
		t.Fatal("non-numeric input should not report OK")
	}
}

// CPUs is divided by everywhere it is used, so it must never return zero.
func TestCPUsIsAtLeastOne(t *testing.T) {
	if n := CPUs(); n < 1 {
		t.Fatalf("CPUs() = %d, want at least 1", n)
	}
}
