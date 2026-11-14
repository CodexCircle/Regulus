package hub

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"testing"
	"testing/fstest"
	"time"

	"github.com/CodexCircle/Regulus/internal/config"
	"github.com/CodexCircle/Regulus/internal/update"
)

// failingListener makes http.Server.Serve return an error at once.
type failingListener struct{ err error }

func (l failingListener) Accept() (net.Conn, error) { return nil, l.err }
func (l failingListener) Close() error              { return nil }
func (l failingListener) Addr() net.Addr            { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1} }

// noReleases is a release feed with nothing in it, so the test never reaches
// for the network.
type noReleases struct{}

func (noReleases) Releases(context.Context) ([]update.Release, error) { return nil, nil }

// When the HTTP server fails by itself nobody cancels the context, and Serve
// used to wait for its background work (which only ends with the context)
// forever, so the process neither served nor exited, and systemd never
// restarted it.
func TestServeReturnsTheErrorOfTheHTTPServer(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	useTestDatabase(t, &cfg)
	cfg.Updates.Check = true    // the update loop runs until its context ends: that is what Serve waited for
	ctx := context.Background() // never cancelled, like the context of a process that was not signalled
	h, err := New(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), fstest.MapFS{}, WithVersion("9.9.9"), WithUpdateSource(noReleases{}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })

	want := errors.New("the listener broke")
	done := make(chan error, 1)
	go func() { done <- h.Serve(ctx, failingListener{err: want}) }()
	select {
	case err := <-done:
		if !errors.Is(err, want) {
			t.Errorf("Serve returned %v, want %v", err, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after the HTTP server failed")
	}
}
