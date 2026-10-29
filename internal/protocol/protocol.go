// Package protocol holds the version of the agent-hub contract defined in
// proto/regulus/agent/v1 and the rules to compare versions. The generated code
// lives in internal/gen and must not be edited by hand (`make proto`).
package protocol

// Version is the protocol version spoken by this build. It is sent in Hello
// and Welcome. Bump it only for changes that an older peer cannot understand;
// adding fields or messages is compatible and does not need a new version.
const Version uint32 = 1

// MinSupported is the oldest protocol version this build still accepts.
const MinSupported uint32 = 1

// Supported reports whether a peer speaking version v can be served.
func Supported(v uint32) bool {
	return v >= MinSupported && v <= Version
}

// Negotiate picks the protocol version for a connection from the range the
// agent speaks. min 0 means an agent that predates the field and speaks only
// max. ok is false when the ranges do not meet.
func Negotiate(min, max uint32) (version uint32, ok bool) {
	if min == 0 || min > max {
		min = max
	}
	version = max
	if version > Version {
		version = Version
	}
	return version, version >= min && Supported(version)
}
