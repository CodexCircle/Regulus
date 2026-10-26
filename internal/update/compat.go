package update

// Compat tells how the release of an agent relates to the hub's.
type Compat string

const (
	// CompatCurrent: the same release.
	CompatCurrent Compat = "current"
	// CompatBehind: older than the hub but still supported (the previous
	// minor release, or any release of the previous major). It connects; the
	// dashboard suggests updating it.
	CompatBehind Compat = "behind"
	// CompatAhead: newer than the hub. The hub is updated first, so this
	// means the hub is the one to update. It connects if the protocol allows.
	CompatAhead Compat = "ahead"
	// CompatTooOld: further behind than the hub supports. It is refused.
	CompatTooOld Compat = "too_old"
	// CompatUnknown: one of the two is not a release build (a development
	// build, or a version that cannot be read). It connects.
	CompatUnknown Compat = "unknown"
)

// CheckCompat applies the rule "the hub accepts agents of its own release
// series and the previous one": the same major and minor, one minor behind,
// or any release of the previous major. The protocol version is negotiated
// separately and can still refuse an agent this accepts.
func CheckCompat(hubVersion, agentVersion string) Compat {
	hub, ok := ParseBuildVersion(hubVersion)
	if !ok {
		return CompatUnknown
	}
	agent, ok := ParseBuildVersion(agentVersion)
	if !ok {
		return CompatUnknown
	}
	switch c := agent.Compare(hub); {
	case c == 0:
		return CompatCurrent
	case c > 0:
		return CompatAhead
	}
	// The agent is older.
	switch {
	case agent.Major == hub.Major && agent.Minor == hub.Minor:
		return CompatBehind // only a patch or pre-release behind
	case agent.Major == hub.Major && hub.Minor-agent.Minor == 1:
		return CompatBehind
	case agent.Major+1 == hub.Major:
		return CompatBehind
	}
	return CompatTooOld
}
