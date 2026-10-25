package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/CodexCircle/Regulus/internal/auth"
	"github.com/CodexCircle/Regulus/internal/config"
	"github.com/CodexCircle/Regulus/internal/logging"
	"github.com/CodexCircle/Regulus/internal/storage"
)

// runResetAdmin implements `regulus hub reset-admin`: it gives back access to
// a hub whose admin cannot log in. Whoever can read and write the data
// directory owns the hub, so no further proof is asked.
func runResetAdmin(args []string) int {
	return resetAdmin(args, os.Getenv, os.Stdout, os.Stderr)
}

func resetAdmin(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	var user string
	cfg, err := config.LoadWith("hub reset-admin", args, getenv, stderr, func(fs *flag.FlagSet) {
		fs.StringVar(&user, "user", "", "the admin to recover (default: the only admin)")
		fs.Usage = func() {
			_, _ = fmt.Fprint(fs.Output(), `Usage: regulus hub reset-admin [--config FILE] [--data-dir DIR] [--user NAME]

Makes a one-time link, valid for one hour, to choose a new password for an
admin account, enables the account if it was disabled, and ends all its
sessions. The password is chosen in the dashboard, so it never appears in a
terminal. Run it as the user that owns the data directory (for the package
install: sudo -u regulus regulus hub reset-admin --config
/etc/regulus/config.yaml), on the machine where the
hub runs; the hub may keep running.

Flags:
`)
			fs.PrintDefaults()
		}
	})
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "regulus: %v\n", err)
		return 2
	}
	if os.Geteuid() == 0 {
		_, _ = fmt.Fprintln(stderr, "warning: running as root; if the hub runs as another user, run this as that user (for example sudo -u regulus ...) so the database files keep their owner")
	}

	// Do not create an empty database where there is none: that would be a
	// wrong data directory, not a recovery.
	dbFile := filepath.Join(cfg.DataDir, storage.FileName)
	if _, err := os.Stat(dbFile); err != nil {
		_, _ = fmt.Fprintf(stderr, "regulus: no hub database at %s: check --data-dir (or data_dir in the configuration)\n", dbFile)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db, err := storage.Open(ctx, cfg.DataDir)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "regulus: %v\n", err)
		return 1
	}
	defer func() { _ = db.Close() }()

	log, err := logging.New(cfg.Log, io.Discard)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "regulus: %v\n", err)
		return 2
	}
	rec, err := auth.RecoverAdmin(ctx, db, user, log)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "regulus: %v\n", err)
		return 1
	}

	if rec.TwoFactorCleared {
		_, _ = fmt.Fprintf(stderr, "Two-factor authentication of %q was removed; set it up again from the Account page.\n", rec.Username)
	}
	if rec.Reenabled {
		_, _ = fmt.Fprintf(stderr, "The account %q was disabled and is enabled again.\n", rec.Username)
	}
	_, _ = fmt.Fprintf(stderr, "Open this link to choose a new password for %q. It works once and expires at %s.\nAll sessions of the account were ended. Replace the host if you reach the dashboard under another name.\n\n",
		rec.Username, rec.Expires.Local().Format(time.RFC1123))
	_, _ = fmt.Fprintf(stdout, "%s/#/reset/%s\n", dashboardURL(cfg.Listen), rec.Token)
	return 0
}

// dashboardURL guesses where the dashboard is from the listen address.
func dashboardURL(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "http://localhost:8080"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, port)
}
