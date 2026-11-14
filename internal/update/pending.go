package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// pendingName is the marker in the state directory (root's own, so nothing in
// it can be forged by the hub's user) that exists while an update is between
// the replacement of the executable and a confirmed outcome.
const pendingName = "pending.json"

// Pending describes an update that was started and not finished. It is written
// before the executable is replaced and removed when the update ended, applied
// or rolled back. A helper that is killed in between (out of memory, a stop,
// its time limit, a restart of the machine) leaves it behind, and Recover
// finishes the job.
type Pending struct {
	From    string    `json:"from"`
	To      string    `json:"to"`
	Backup  string    `json:"backup"`
	Unit    string    `json:"unit"`
	Agent   bool      `json:"agent"`
	DataDir string    `json:"data_dir"`
	Started time.Time `json:"started"`
}

func (in *Installer) pendingPath() string { return filepath.Join(in.StateDir, pendingName) }

// writePending records the update on disk before anything is replaced: the
// file and its directory are synced, so the marker survives a power cut that
// the replacement it announces also survives.
func (in *Installer) writePending(p Pending) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(in.StateDir, ".pending-")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), in.pendingPath()); err != nil {
		return err
	}
	syncDir(in.StateDir, in.Log)
	return nil
}

func (in *Installer) clearPending() {
	if err := os.Remove(in.pendingPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		in.Log.Error("removing the marker of the update in progress", "err", err)
	}
	syncDir(in.StateDir, in.Log)
}

// syncDir flushes a directory so that a rename in it survives a power cut. A
// failure is logged, not fatal: some file systems do not support it.
func syncDir(dir string, log *slog.Logger) {
	d, err := os.Open(dir)
	if err != nil {
		log.Warn("syncing a directory", "dir", dir, "err", err)
		return
	}
	defer func() { _ = d.Close() }()
	if err := d.Sync(); err != nil {
		log.Warn("syncing a directory", "dir", dir, "err", err)
	}
}

// Recover finishes an update that an earlier run left unfinished. It does
// nothing, and returns false, when there is no marker. Otherwise it settles
// what is on disk and what is running:
//
//   - the executable is the new version: the service is restarted and watched
//     as in a normal update; healthy means the update stands, otherwise the
//     previous version is put back;
//   - anything else (the previous version still there, a missing or damaged
//     file): the previous version is put back from the copy kept for that.
//
// The marker is always removed at the end, even when putting the previous
// version back failed: a marker that stays would start the helper again and
// again. A failure is in the result and in the exit status.
func (in *Installer) Recover(ctx context.Context) (ApplyResult, bool, error) {
	in.defaults()
	data, err := readRegular(in.pendingPath(), maxControlFile)
	if errors.Is(err, os.ErrNotExist) {
		return ApplyResult{}, false, nil
	}
	dir := ""
	var p Pending
	if err == nil {
		err = json.Unmarshal(data, &p)
	}
	if err != nil || p.To == "" || p.From == "" || p.Backup == "" {
		in.clearPending()
		return ApplyResult{Outcome: OutcomeFailed, Message: "an unreadable marker of an interrupted update was removed", At: in.Now().UTC()}, true,
			errors.New("the marker of an interrupted update could not be read: it was removed")
	}
	// What the marker says about the role is what the interrupted run was.
	in.Unit, in.Agent, in.DataDir = p.Unit, p.Agent, p.DataDir
	dir = ApplyDir(p.DataDir)
	if filepath.Dir(p.Backup) != filepath.Clean(in.StateDir) {
		in.clearPending()
		return ApplyResult{Outcome: OutcomeFailed, Message: "the marker of an interrupted update points outside the state directory and was removed", At: in.Now().UTC()}, true,
			errors.New("the marker of an interrupted update names a file outside the state directory: it was removed")
	}
	res := ApplyResult{Version: p.To, From: p.From, At: in.Now().UTC()}
	finish := func(outcome string, err error) (ApplyResult, bool, error) {
		in.clearPending()
		res.Outcome, res.At = outcome, in.Now().UTC()
		if err != nil {
			res.Message = err.Error()
		}
		if p.DataDir != "" {
			if werr := WriteResult(dir, res); werr != nil {
				in.Log.Error("writing the result", "err", werr)
			}
		}
		in.Log.Info("interrupted update settled", "outcome", outcome, "from", p.From, "to", p.To, "err", err)
		return res, true, err
	}

	in.Log.Warn("an update was interrupted; settling it", "from", p.From, "to", p.To)
	if v, verr := in.installedVersion(ctx); verr == nil && v.String() == p.To {
		werr := in.restartAndWatch(ctx, p.To)
		if werr == nil {
			res.Message = "the helper was interrupted; the new version was checked afterwards and stands"
			return finish(OutcomeApplied, nil)
		}
		in.Log.Error("the interrupted update is not healthy; rolling back", "err", werr)
	} else if verr == nil && v.String() == p.From {
		// The swap never happened (or was undone): nothing to put back.
		return finish(OutcomeFailed, fmt.Errorf("the helper was interrupted before the new version was installed; %s is still in place", p.From))
	}
	rctx, cancel := in.rollbackContext(ctx)
	defer cancel()
	if rerr := in.rollback(rctx, p.Backup, p.From); rerr != nil {
		return finish(OutcomeFailed, fmt.Errorf("the helper was interrupted and putting %s back failed: %v", p.From, rerr))
	}
	return finish(OutcomeRolledBack, fmt.Errorf("the helper was interrupted; version %s was put back", p.From))
}
