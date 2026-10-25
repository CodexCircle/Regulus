package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CodexCircle/Regulus/internal/auth"
	"github.com/CodexCircle/Regulus/internal/storage"
)

func noEnv(string) string { return "" }

// hubWithAdmin makes a data directory with one admin and returns it.
func hubWithAdmin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	ctx := context.Background()
	db, err := storage.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	s, err := auth.New(ctx, db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.Setup(ctx, s.SetupToken(), "admin", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestResetAdminPrintsALinkThatWorks(t *testing.T) {
	dir := hubWithAdmin(t)
	var out, errOut bytes.Buffer
	if code := resetAdmin([]string{"--data-dir", dir, "--listen", "0.0.0.0:9000"}, noEnv, &out, &errOut); code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, errOut.String())
	}
	link := strings.TrimSpace(out.String())
	const prefix = "http://localhost:9000/#/reset/"
	if !strings.HasPrefix(link, prefix) || strings.Count(out.String(), "\n") != 1 {
		t.Fatalf("stdout = %q, want one line starting with %s", out.String(), prefix)
	}

	// The token in the link is accepted by the hub's service.
	ctx := context.Background()
	db, err := storage.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	s, err := auth.New(ctx, db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ResetPassword(ctx, "r", strings.TrimPrefix(link, prefix), "a brand new password"); err != nil {
		t.Errorf("the printed token does not work: %v", err)
	}
}

func TestResetAdminRefusesToInventADatabase(t *testing.T) {
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	if code := resetAdmin([]string{"--data-dir", dir}, noEnv, &out, &errOut); code == 0 {
		t.Fatal("a missing database must fail")
	}
	if !strings.Contains(errOut.String(), "no hub database") || out.Len() != 0 {
		t.Errorf("stderr = %q, stdout = %q", errOut.String(), out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, storage.FileName)); err == nil {
		t.Error("the command created a database")
	}
}

func TestResetAdminRejectsAWrongAccount(t *testing.T) {
	dir := hubWithAdmin(t)
	var out, errOut bytes.Buffer
	if code := resetAdmin([]string{"--data-dir", dir, "--user", "ghost"}, noEnv, &out, &errOut); code == 0 {
		t.Fatal("an unknown account must fail")
	}
	if out.Len() != 0 || !strings.Contains(errOut.String(), "not an admin account") {
		t.Errorf("stderr = %q, stdout = %q", errOut.String(), out.String())
	}
}
