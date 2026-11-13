package storage_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/CodexCircle/Regulus/internal/storage"
)

// The TLS tests need a PostgreSQL server that has TLS on and whose certificate
// names the host in REGULUS_TEST_TLS_URL (and only that host). CI starts one
// (job "PostgreSQL with TLS"). Without the variables the tests are skipped.
const (
	envTLSURL = "REGULUS_TEST_TLS_URL" // postgres://user:password@localhost:5432/db
	envTLSCA  = "REGULUS_TEST_TLS_CA"  // path of the CA file that signed the server certificate
)

func tlsConfig(t *testing.T) storage.Config {
	t.Helper()
	raw, ca := os.Getenv(envTLSURL), os.Getenv(envTLSCA)
	if raw == "" || ca == "" {
		t.Skipf("%s and %s are not set", envTLSURL, envTLSCA)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || len(u.Path) < 2 {
		t.Fatalf("%s is not a postgres:// URL with a host and a database", envTLSURL)
	}
	port := 5432
	if p := u.Port(); p != "" {
		if port, err = strconv.Atoi(p); err != nil {
			t.Fatalf("%s has a bad port", envTLSURL)
		}
	}
	pass, _ := u.User.Password()
	if pass == "" {
		t.Fatalf("%s must carry a password (the test checks that it is never shown)", envTLSURL)
	}
	return storage.Config{
		Type: storage.TypePostgres, DataDir: t.TempDir(),
		Host: u.Hostname(), Port: port, Name: u.Path[1:], User: u.User.Username(), Password: pass,
		SSLMode: "verify-full", SSLRootCert: ca,
	}
}

// verify-full with the right CA and the host name in the certificate works,
// through Check and through Open, and the session really is encrypted.
func TestPostgresVerifyFullWorks(t *testing.T) {
	cfg := tlsConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := storage.Check(ctx, cfg); err != nil {
		t.Fatalf("Check with verify-full: %v", err)
	}
	cfg.Schema = "tls_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	admin := cfg
	admin.Schema = ""
	raw, err := storage.OpenRaw(admin)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	if _, err := raw.ExecContext(ctx, `CREATE SCHEMA `+cfg.Schema); err != nil {
		t.Fatalf("create schema: %s", storage.Explain(err))
	}
	defer func() { _, _ = raw.Exec(`DROP SCHEMA IF EXISTS ` + cfg.Schema + ` CASCADE`) }()

	db, err := storage.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("Open with verify-full: %v", err)
	}
	defer func() { _ = db.Close() }()
	var encrypted bool
	if err := db.QueryRowContext(ctx, `SELECT ssl FROM pg_stat_ssl WHERE pid = pg_backend_pid()`).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if !encrypted {
		t.Error("the session is not encrypted")
	}
}

// verify-full must refuse a server whose certificate does not name the host
// used, and a server whose certificate the given root did not sign, and the
// message must not carry the password.
func TestPostgresVerifyFullRefuses(t *testing.T) {
	good := tlsConfig(t)

	wrongHost := good
	wrongHost.Host = "127.0.0.1" // the certificate names localhost only
	if wrongHost.Host == good.Host {
		t.Skip("the test URL already uses 127.0.0.1; use a host name that the certificate names")
	}

	otherCA := good
	otherCA.SSLRootCert = writeUnrelatedCA(t)

	noRoot := good
	noRoot.SSLRootCert = filepath.Join(t.TempDir(), "missing.pem")

	for name, tc := range map[string]struct {
		cfg  storage.Config
		want string // part of the explanation Check must give
	}{
		"wrong host name":             {wrongHost, "failed to verify certificate"},
		"root that is not the issuer": {otherCA, "failed to verify certificate"},
		"root file missing":           {noRoot, ""},
	} {
		cfg := tc.cfg
		t.Run(name, func(t *testing.T) {
			// Check tries once. Open would keep trying for a minute, so it gets a
			// short deadline: it must fail and say so, not connect.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			openCtx, openCancel := context.WithTimeout(ctx, 2*time.Second)
			defer openCancel()

			rep, err := storage.Check(ctx, cfg)
			if err == nil {
				t.Fatal("Check connected although the certificate cannot be verified")
			}
			t.Logf("Check: %v", err)
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Check failed for another reason than the certificate: %v", err)
			}
			assertNoSecret(t, "Check error", err.Error(), cfg.Password)
			assertNoSecret(t, "Check report", rep.Where, cfg.Password)

			db, err := storage.Open(openCtx, cfg)
			if err == nil {
				_ = db.Close()
				t.Fatal("Open connected although the certificate cannot be verified")
			}
			t.Logf("Open: %v", err)
			assertNoSecret(t, "Open error", err.Error(), cfg.Password)
		})
	}
}

func assertNoSecret(t *testing.T, what, text, secret string) {
	t.Helper()
	if strings.Contains(text, secret) {
		t.Errorf("%s shows the password: %s", what, text)
	}
}

// writeUnrelatedCA writes a valid self-signed CA certificate that signed
// nothing the server uses, and returns its path.
func writeUnrelatedCA(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Unrelated test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "other-ca.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
