package logging

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/CodexCircle/Regulus/internal/config"
)

func TestJSONFormatAndLevel(t *testing.T) {
	var buf bytes.Buffer
	log, err := New(config.Log{Level: "warn", Format: "json"}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	log.Info("hidden")
	log.Warn("shown", "k", "v")

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("output is not a single JSON record: %q: %v", buf.String(), err)
	}
	if rec["msg"] != "shown" || rec["k"] != "v" {
		t.Fatalf("unexpected record: %v", rec)
	}
}

func TestTextFormat(t *testing.T) {
	var buf bytes.Buffer
	log, err := New(config.Log{Level: "info", Format: "text"}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	log.Info("hello")
	if !strings.Contains(buf.String(), "msg=hello") {
		t.Fatalf("unexpected output: %q", buf.String())
	}
}

func TestInvalid(t *testing.T) {
	if _, err := New(config.Log{Level: "loud", Format: "text"}, &bytes.Buffer{}); err == nil {
		t.Fatal("expected an error for an invalid level")
	}
	if _, err := New(config.Log{Level: "info", Format: "xml"}, &bytes.Buffer{}); err == nil {
		t.Fatal("expected an error for an invalid format")
	}
}
