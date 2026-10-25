// Package protocol holds the version of the agent-hub contract defined in
// proto/regulus/agent/v1. The generated code lives in internal/gen.
package protocol

// Version is the protocol version spoken by this build. It is sent in Hello
// and Welcome. Bump it only for changes that an older peer cannot understand;
// adding fields or messages is compatible and does not need a new version.
const Version uint32 = 1
