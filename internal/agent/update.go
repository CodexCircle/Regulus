package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	agentv1 "github.com/CodexCircle/Regulus/internal/gen/regulus/agent/v1"
	"github.com/CodexCircle/Regulus/internal/update"
)

// applyUpdate runs the ApplyUpdate command: fetch the release from the hub,
// check it, and leave a request for the helper that runs as root on this
// server. The agent never installs anything itself. The result says whether the
// request was handed over; the update is proven later by the restarted agent
// connecting with the new version.
func (a *agent) applyUpdate(ctx context.Context, id string, au *agentv1.ApplyUpdate) *agentv1.CommandResult {
	res := &agentv1.CommandResult{CommandId: id}
	if err := a.stageUpdate(ctx, au); err != nil {
		a.log.Warn("cannot update", "version", au.GetVersion(), "err", err)
		res.Error = err.Error()
		return res
	}
	a.log.Info("update handed to the installer", "version", au.GetVersion())
	res.Success = true
	return res
}

func (a *agent) stageUpdate(ctx context.Context, au *agentv1.ApplyUpdate) error {
	dir := a.opts.UpdateDir
	if dir == "" {
		return errors.New("this agent is not set up to update itself (REGULUS_AGENT_UPDATES, and the regulus-agent-update.path unit that the installer enables)")
	}
	want, err := update.ParseVersion(au.GetVersion())
	if err != nil {
		return fmt.Errorf("the hub offered an invalid version: %w", err)
	}
	have, ok := update.ParseBuildVersion(a.opts.Version)
	if !ok {
		return fmt.Errorf("this agent is a development build (%s) and is never updated", a.opts.Version)
	}
	if want.Compare(have) <= 0 {
		return fmt.Errorf("version %s is not newer than %s", want, have)
	}
	plat := update.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH}
	if au.GetArchive() != update.ArchiveName(want, plat.OS, plat.Arch) {
		return fmt.Errorf("the hub offered %q, which is not the build for %s/%s", au.GetArchive(), plat.OS, plat.Arch)
	}
	applyDir := update.ApplyDir(dir)
	if _, pending := update.ReadRequest(applyDir); pending {
		return errors.New("an update is already waiting to be installed")
	}

	stager := &update.Stager{Dir: filepath.Join(dir, "updates"), Verifier: a.opts.Verifier}
	staged, err := stager.StageFetched(ctx, want, plat, au.GetSha256(), func(ctx context.Context, file string, w io.Writer) error {
		return a.fetchUpdateFile(ctx, want.String(), file, w)
	})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(applyDir, 0o700); err != nil {
		return err
	}
	return update.WriteRequest(applyDir, update.ApplyRequest{Version: staged.Version, RequestedBy: "hub", RequestedAt: time.Now().UTC()})
}

// fetchUpdateFile copies one file of the offered release from the hub.
func (a *agent) fetchUpdateFile(ctx context.Context, version, file string, w io.Writer) error {
	kind := agentv1.UpdateFile_UPDATE_FILE_ARCHIVE
	switch file {
	case update.FileChecksums:
		kind = agentv1.UpdateFile_UPDATE_FILE_CHECKSUMS
	case update.FileBundle:
		kind = agentv1.UpdateFile_UPDATE_FILE_BUNDLE
	}
	stream, err := a.client.FetchUpdate(ctx, &agentv1.FetchUpdateRequest{Version: version, File: kind})
	if err != nil {
		return err
	}
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := w.Write(chunk.GetData()); err != nil {
			return err
		}
	}
}

// markConnected tells the helper, after an update, that the hub accepted this
// agent with this version.
func (a *agent) markConnected() {
	if a.opts.UpdateDir == "" {
		return
	}
	if err := update.WriteConnected(a.opts.UpdateDir, update.Connected{Version: a.opts.Version, At: time.Now().UTC()}); err != nil {
		a.log.Warn("recording the connection for the updater", "err", err)
	}
}
