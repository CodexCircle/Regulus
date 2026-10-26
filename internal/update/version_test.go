package update

import "testing"

func TestParseVersion(t *testing.T) {
	good := map[string]string{
		"1.4.0":              "1.4.0",
		"v1.4.0":             "1.4.0",
		" v0.1.0 ":           "0.1.0",
		"v1.5.0-beta.1":      "1.5.0-beta.1",
		"1.0.0-rc.1+build.5": "1.0.0-rc.1",
		"2.0.0+meta":         "2.0.0",
	}
	for in, want := range good {
		v, err := ParseVersion(in)
		if err != nil || v.String() != want {
			t.Errorf("ParseVersion(%q) = %q, %v; want %q", in, v, err, want)
		}
	}
	for _, in := range []string{"", "dev", "1.4", "1.4.0.1", "01.4.0", "1.4.0-", "1.4.0-01", "v1.x.0", "plugin/x/v1.0.0", "1.4.0-beta..1", "99999999999999999999.0.0"} {
		if v, err := ParseVersion(in); err == nil {
			t.Errorf("ParseVersion(%q) = %q, want an error", in, v)
		}
	}
}

// The order is the one in the SemVer 2.0 specification, section 11.
func TestCompareFollowsSemVer(t *testing.T) {
	ordered := []string{
		"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1",
		"1.0.0", "1.0.1", "1.1.0", "1.10.0", "2.0.0",
	}
	for i, a := range ordered {
		for j, b := range ordered {
			va, _ := ParseVersion(a)
			vb, _ := ParseVersion(b)
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got := va.Compare(vb); got != want {
				t.Errorf("Compare(%s, %s) = %d, want %d", a, b, got, want)
			}
		}
	}
}

func TestBuildMetadataIsIgnoredWhenComparing(t *testing.T) {
	a, _ := ParseVersion("1.0.0+a")
	b, _ := ParseVersion("1.0.0+b")
	if a.Compare(b) != 0 {
		t.Error("1.0.0+a and 1.0.0+b should compare equal")
	}
}

func TestParseBuildVersion(t *testing.T) {
	for in, release := range map[string]bool{
		"0.2.0":                   true,
		"v0.2.0":                  true,
		"0.3.0-beta.1":            true,
		"dev":                     false,
		"":                        false,
		"v0.1.0-3-g1a2b3c4":       false, // git describe, three commits after the tag
		"v0.1.0-3-g1a2b3c4-dirty": false,
		"abc1234":                 false,
	} {
		if _, ok := ParseBuildVersion(in); ok != release {
			t.Errorf("ParseBuildVersion(%q) ok = %v, want %v", in, ok, release)
		}
	}
}
