package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/CodexCircle/Regulus/internal/update"
)

// runUpdateApply is the root-owned half of the updates: systemd starts it
// (regulus-update.service) when the hub leaves a request. It checks the
// downloaded release again, installs it, restarts the service and puts the
// previous version back if the new one does not come up healthy.
func runUpdateApply(args []string) int {
	fs := flag.NewFlagSet("update-apply", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dataDir := fs.String("data-dir", "/var/lib/regulus", "the hub's data directory, where the request and the downloaded release are")
	stateDir := fs.String("state-dir", "/var/lib/regulus-update", "directory owned by root for the previous binary and the signature trust cache")
	unit := fs.String("unit", "regulus.service", "the service to restart")
	binary := fs.String("binary", "", "the executable to replace (default: this one)")
	isAgent := fs.Bool("agent", false, "this server runs the agent: healthy means the agent connected to the hub again")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "regulus: update-apply installs files owned by root; systemd runs it as root")
		return 1
	}
	if *binary == "" {
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "regulus: %v\n", err)
			return 1
		}
		if *binary, err = filepath.EvalSymlinks(exe); err != nil {
			fmt.Fprintf(os.Stderr, "regulus: %v\n", err)
			return 1
		}
	}
	if err := os.MkdirAll(*stateDir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "regulus: %v\n", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()

	in := &update.Installer{
		DataDir:  *dataDir,
		StateDir: *stateDir,
		Binary:   *binary,
		Unit:     *unit,
		Agent:    *isAgent,
		Verifier: &update.SigstoreVerifier{Trust: update.PublicTrust(filepath.Join(*stateDir, "tuf"))},
	}
	res, err := in.Apply(ctx)
	switch {
	case errors.Is(err, update.ErrNoRequest):
		fmt.Fprintln(os.Stderr, "regulus: there is no update request")
		return 0
	case err != nil:
		fmt.Fprintf(os.Stderr, "regulus: update to %s: %s: %v\n", res.Version, res.Outcome, err)
		return 1
	}
	fmt.Printf("regulus: updated from %s to %s\n", res.From, res.Version)
	return 0
}
