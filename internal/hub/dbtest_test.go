package hub

import (
	"testing"

	"github.com/CodexCircle/Regulus/internal/config"
	"github.com/CodexCircle/Regulus/internal/storage/storagetest"
)

// useTestDatabase points the hub of a test at PostgreSQL when
// REGULUS_TEST_DATABASE_URL is set, so the whole hub runs on both backends.
func useTestDatabase(t *testing.T, cfg *config.Config) {
	t.Helper()
	if !storagetest.Postgres() {
		return
	}
	s := storagetest.Config(t, cfg.DataDir)
	cfg.Database = config.Database{
		Type: s.Type, Host: s.Host, Port: s.Port, Name: s.Name, User: s.User,
		Password: s.Password, SSLMode: s.SSLMode, Schema: s.Schema,
	}
}
