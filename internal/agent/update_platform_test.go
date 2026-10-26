package agent

import "runtime"

func goos() string   { return runtime.GOOS }
func goarch() string { return runtime.GOARCH }

// runtimeSkip is true where the agent's own build is not one of the two that
// releases are made for, so the "another build" case would not be refused for
// the reason under test.
func runtimeSkip() bool {
	return runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64")
}
