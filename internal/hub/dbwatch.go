package hub

import (
	"context"
	"sync"
	"time"

	"github.com/CodexCircle/Regulus/internal/storage"
)

// dbWatchInterval is how often the hub checks that the database answers.
const dbWatchInterval = 15 * time.Second

// dbHealth remembers whether the database answered the last time it was asked,
// so the problem can be shown (regulus status, the console) and logged once
// when it starts and once when it ends, not at every check.
type dbHealth struct {
	mu      sync.Mutex
	problem string
}

func (d *dbHealth) set(p string) (changed bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	changed = d.problem != p
	d.problem = p
	return changed
}

func (d *dbHealth) get() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.problem
}

// checkDatabase pings the database once and records the result. database/sql
// opens a new connection by itself after PostgreSQL restarts, so the hub keeps
// running and recovers without a restart; this only reports it.
func (h *Hub) checkDatabase(ctx context.Context) {
	pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	problem := ""
	if err := h.db.PingContext(pctx); err != nil {
		problem = storage.Explain(err)
	}
	if !h.dbState.set(problem) {
		return
	}
	if problem != "" {
		h.log.Error("database is not answering", "backend", h.cfg.Storage().Describe(), "problem", problem)
	} else {
		h.log.Info("database answers again", "backend", h.cfg.Storage().Describe())
	}
}

// watchDatabase checks the database until ctx ends. Only a remote database
// can go away, so SQLite is not polled.
func (h *Hub) watchDatabase(ctx context.Context) {
	if !h.cfg.Storage().IsPostgres() {
		return
	}
	go func() {
		t := time.NewTicker(dbWatchInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				h.checkDatabase(ctx)
			}
		}
	}()
}
