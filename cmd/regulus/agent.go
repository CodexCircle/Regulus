package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/CodexCircle/Regulus/internal/agent"
	"github.com/CodexCircle/Regulus/internal/config"
	"github.com/CodexCircle/Regulus/internal/logging"
)

// exitRejected is the exit status of an agent the hub does not accept (revoked
// or removed). The service unit does not restart on it: retrying cannot help.
const exitRejected = 3

// runAgent implements `regulus agent`: a remote agent that connects out to a
// hub. The first run enrolls it with the token; later runs only need the hub
// address and the saved credentials.
func runAgent(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return agentMain(ctx, args, os.Getenv, os.Stderr)
}

func agentMain(ctx context.Context, args []string, getenv func(string) string, stderr io.Writer) int {
	cfg, err := config.LoadAgent(args, getenv, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "regulus: %v\n", err)
		return 2
	}
	log, err := logging.New(cfg.Log, stderr)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "regulus: %v\n", err)
		return 2
	}

	creds, err := agent.LoadCredentials(cfg.DataDir)
	switch {
	case err == nil:
		if cfg.Token != "" {
			log.Info("already enrolled: the token is ignored", "agent", creds.AgentID)
		}
	case errors.Is(err, agent.ErrNotEnrolled):
		if cfg.Token == "" || cfg.CAFingerprint == "" {
			log.Error("this agent is not enrolled: give the token and the CA fingerprint shown by the dashboard (--token, --ca-fingerprint)")
			return 2
		}
		creds, err = agent.Enroll(ctx, cfg.Hub, cfg.CAFingerprint, cfg.Token, version, cfg.DataDir)
		if err != nil {
			log.Error("enrollment failed", "err", err)
			return 1
		}
		log.Info("enrolled", "agent", creds.AgentID, "hub", cfg.Hub)
	default:
		log.Error("cannot load the agent credentials", "dir", cfg.DataDir, "err", err)
		return 1
	}

	if cfg.EnrollOnly {
		return 0
	}

	conn, err := creds.Dial(cfg.Hub)
	if err != nil {
		log.Error("cannot connect", "err", err)
		return 1
	}
	defer func() { _ = conn.Close() }()

	log.Info("starting agent", "version", version, "agent", creds.AgentID, "hub", cfg.Hub)
	err = agent.Run(ctx, conn, agent.Options{Version: version, ID: creds.AgentID, Log: log})
	var rejected *agent.RejectedError
	if errors.As(err, &rejected) {
		log.Error("the hub does not accept this agent: it was revoked or removed. To use this server again, remove the agent data directory and enroll with a new token", "dir", cfg.DataDir, "err", err)
		return exitRejected
	}
	if err != nil {
		log.Error("agent stopped", "err", err)
		return 1
	}
	return 0
}
