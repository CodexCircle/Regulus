// Package update finds, downloads and verifies new releases of Regulus. The
// hub drives it (see the design in the README): it asks the release source
// what is newest on the chosen channel and keeps a verified copy in a local
// cache, ready for whoever applies it.
package update

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Version is a SemVer 2.0 version.
type Version struct {
	Major, Minor, Patch uint64
	// Pre holds the dot-separated pre-release identifiers, such as ["beta", "1"].
	Pre []string
}

var versionRE = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

// describeRE matches what `git describe` appends to a build made from a
// commit after a tag: -3-g1a2b3c4 and an optional -dirty.
var describeRE = regexp.MustCompile(`-[0-9]+-g[0-9a-f]+(-dirty)?$`)

// ParseVersion reads a version such as "1.4.0", "v1.4.0" or "v1.5.0-beta.1".
// Build metadata (+...) is accepted and ignored, as SemVer says it must be
// when comparing.
func ParseVersion(s string) (Version, error) {
	m := versionRE.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return Version{}, fmt.Errorf("%q is not a version like 1.4.0 or 1.5.0-beta.1", s)
	}
	var v Version
	for i, dst := range []*uint64{&v.Major, &v.Minor, &v.Patch} {
		n, err := strconv.ParseUint(m[i+1], 10, 64)
		if err != nil {
			return Version{}, fmt.Errorf("%q: number too large", s)
		}
		*dst = n
	}
	if m[4] != "" {
		v.Pre = strings.Split(m[4], ".")
		for _, id := range v.Pre {
			if isNumeric(id) && len(id) > 1 && id[0] == '0' {
				return Version{}, fmt.Errorf("%q: numeric pre-release identifier %q has a leading zero", s, id)
			}
		}
	}
	return v, nil
}

// ParseBuildVersion reads the version a binary was built with
// (-ldflags "-X main.version=..."). It reports ok = false for builds that are
// not a release: "dev", or the output of `git describe` for a commit after a
// tag. Such a build cannot tell whether a release is newer.
func ParseBuildVersion(s string) (v Version, ok bool) {
	if describeRE.MatchString(s) {
		return Version{}, false
	}
	v, err := ParseVersion(s)
	return v, err == nil
}

// String returns the version without a leading "v".
func (v Version) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if len(v.Pre) > 0 {
		s += "-" + strings.Join(v.Pre, ".")
	}
	return s
}

// Prerelease reports whether the version is a pre-release (the beta channel).
func (v Version) Prerelease() bool { return len(v.Pre) > 0 }

// Compare returns -1, 0 or 1 as v is older than, equal to or newer than o,
// following SemVer precedence: a pre-release is older than its release.
func (v Version) Compare(o Version) int {
	for _, p := range [][2]uint64{{v.Major, o.Major}, {v.Minor, o.Minor}, {v.Patch, o.Patch}} {
		if c := cmpUint(p[0], p[1]); c != 0 {
			return c
		}
	}
	switch {
	case len(v.Pre) == 0 && len(o.Pre) == 0:
		return 0
	case len(v.Pre) == 0:
		return 1
	case len(o.Pre) == 0:
		return -1
	}
	for i := 0; i < len(v.Pre) && i < len(o.Pre); i++ {
		if c := cmpIdentifier(v.Pre[i], o.Pre[i]); c != 0 {
			return c
		}
	}
	return cmpUint(uint64(len(v.Pre)), uint64(len(o.Pre)))
}

func cmpUint(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func isNumeric(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// cmpIdentifier compares two pre-release identifiers: numbers numerically and
// below words, words in ASCII order.
func cmpIdentifier(a, b string) int {
	an, bn := isNumeric(a), isNumeric(b)
	switch {
	case an && bn:
		// Leading zeros are refused by ParseVersion, so a longer number is larger.
		if c := cmpUint(uint64(len(a)), uint64(len(b))); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	case an:
		return -1
	case bn:
		return 1
	}
	return strings.Compare(a, b)
}
