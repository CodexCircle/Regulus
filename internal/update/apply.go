package update

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Installing an update takes root, which the hub does not have. The two sides
// talk through a directory the hub owns (ApplyDir): the hub writes a request,
// systemd notices it (regulus-update.path) and starts the helper, `regulus
// update-apply`, which re-checks everything itself and writes a result. The
// request names a version and nothing else; the helper never takes a path,
// a command or a checksum from it.

const (
	requestName = "request.json"
	resultName  = "result.json"

	// maxControlFile bounds what is read from the hub-owned directory.
	maxControlFile = 64 << 10
)

// Outcomes of an installation.
const (
	// OutcomeInstalling is written when the helper starts and replaced when it
	// ends.
	OutcomeInstalling = "installing"
	OutcomeApplied    = "applied"
	// OutcomeRolledBack: the new version was installed but did not come up
	// healthy, so the previous one was put back.
	OutcomeRolledBack = "rolled_back"
	// OutcomeFailed: nothing was changed.
	OutcomeFailed = "failed"
)

// ApplyRequest asks the helper to install a staged release.
type ApplyRequest struct {
	Version     string    `json:"version"`
	RequestedBy string    `json:"requested_by"`
	RequestedAt time.Time `json:"requested_at"`
}

// ApplyResult is what the helper reports.
type ApplyResult struct {
	Version     string    `json:"version"`
	From        string    `json:"from,omitempty"`
	Outcome     string    `json:"outcome"`
	Message     string    `json:"message,omitempty"`
	RequestedBy string    `json:"requested_by,omitempty"`
	At          time.Time `json:"at"`
}

// ApplyDir is the directory, inside the hub's data directory, that the hub and
// the helper share. systemd watches the request file in it.
func ApplyDir(dataDir string) string { return filepath.Join(dataDir, "update-apply") }

// WriteRequest leaves a request for the helper. The file appears all at once.
func WriteRequest(dir string, req ApplyRequest) error {
	return writeJSON(filepath.Join(dir, requestName), req, 0o600)
}

// ReadRequest returns the request waiting for the helper, if any.
func ReadRequest(dir string) (ApplyRequest, bool) {
	var req ApplyRequest
	ok := readJSON(filepath.Join(dir, requestName), &req)
	return req, ok
}

// WriteResult records the outcome. It is readable by the hub's user.
func WriteResult(dir string, res ApplyResult) error {
	return writeJSON(filepath.Join(dir, resultName), res, 0o644)
}

// ReadResult returns the last outcome the helper wrote, if any.
func ReadResult(dir string) (ApplyResult, bool) {
	var res ApplyResult
	ok := readJSON(filepath.Join(dir, resultName), &res)
	return res, ok
}

func readJSON(path string, v any) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	return json.NewDecoder(limited(f, maxControlFile)).Decode(v) == nil
}

func writeJSON(path string, v any, mode os.FileMode) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// ErrNoRequest is returned by the installer when the hub left nothing to do.
var ErrNoRequest = errors.New("there is no update request")

// ConnectedName is the file an agent writes when the hub accepted it.
const ConnectedName = "connected.json"

// Connected is written by an agent each time the hub accepts it. After an
// update the helper reads it: an agent that connected again with the new
// version is a healthy one.
type Connected struct {
	Version string    `json:"version"`
	At      time.Time `json:"at"`
}

// WriteConnected records that the agent of dataDir is connected.
func WriteConnected(dataDir string, c Connected) error {
	return writeJSON(filepath.Join(dataDir, ConnectedName), c, 0o644)
}

// ReadConnected returns what the agent of dataDir last wrote.
func ReadConnected(dataDir string) (Connected, bool) {
	var c Connected
	return c, readJSON(filepath.Join(dataDir, ConnectedName), &c)
}
