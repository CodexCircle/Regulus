// Command regulus is the single Regulus executable. The role it plays is
// chosen with a subcommand; for now only `hub` is implemented.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/CodexCircle/Regulus/internal/config"
	"github.com/CodexCircle/Regulus/internal/hub"
	"github.com/CodexCircle/Regulus/internal/logging"
	"github.com/CodexCircle/Regulus/internal/webui"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "hub":
		os.Exit(runHub(os.Args[2:]))
	case "version":
		fmt.Println(version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "regulus: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `Usage: regulus <command> [flags]

Commands:
  hub      run the hub (serves the dashboard); see 'regulus hub -h' for flags
  version  print the version
`)
}

func runHub(args []string) int {
	cfg, err := config.Load(args, os.Getenv, os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "regulus: %v\n", err)
		return 2
	}
	log, err := logging.New(cfg.Log, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "regulus: %v\n", err)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Info("starting hub", "version", version)
	h, err := hub.New(ctx, cfg, log, webui.FS())
	if err != nil {
		log.Error("cannot start hub", "err", err)
		return 1
	}
	defer h.Close()

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		log.Error("cannot listen", "addr", cfg.Listen, "err", err)
		return 1
	}
	if err := h.Serve(ctx, ln); err != nil {
		log.Error("hub stopped", "err", err)
		return 1
	}
	return 0
}
