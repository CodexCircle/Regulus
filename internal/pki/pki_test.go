package pki

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCAIsCreatedOnceAndReloaded(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pki")
	now := time.Now()
	first, err := LoadOrCreate(dir, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreate(dir, now)
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint() != second.Fingerprint() {
		t.Error("the CA changed between loads")
	}
	if !strings.HasPrefix(first.Fingerprint(), "sha256:") || len(first.Fingerprint()) != len("sha256:")+64 {
		t.Errorf("fingerprint = %q", first.Fingerprint())
	}
	info, err := os.Stat(filepath.Join(dir, "ca.key"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("ca.key mode = %v, %v; want 0600", info.Mode().Perm(), err)
	}
}

func TestHalfACAIsAnError(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadOrCreate(dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "ca.key")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreate(dir, time.Now()); err == nil {
		t.Error("a CA without its key must not be replaced silently")
	}
}

func TestParseFingerprint(t *testing.T) {
	good := strings.Repeat("ab", 32)
	for _, in := range []string{good, "sha256:" + good, "SHA256:" + strings.ToUpper(good), " " + good + "\n"} {
		got, err := ParseFingerprint(in)
		if err != nil || got != "sha256:"+good {
			t.Errorf("ParseFingerprint(%q) = %q, %v", in, got, err)
		}
	}
	colons := strings.TrimSuffix(strings.Repeat("ab:", 32), ":")
	if got, err := ParseFingerprint(colons); err != nil || got != "sha256:"+good {
		t.Errorf("with colons: %q, %v", got, err)
	}
	for _, in := range []string{"", "abc", good + "00", strings.Repeat("zz", 32)} {
		if _, err := ParseFingerprint(in); err == nil {
			t.Errorf("ParseFingerprint(%q) must fail", in)
		}
	}
}

func TestSignClientIgnoresTheRequestedSubject(t *testing.T) {
	ca, _ := LoadOrCreate(t.TempDir(), time.Now())
	_, csr, err := NewAgentKey()
	if err != nil {
		t.Fatal(err)
	}
	der, serial, err := ca.SignClient(csr, "agent-1234", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	if cert.Subject.CommonName != "agent-1234" || cert.SerialNumber.Text(16) != serial {
		t.Errorf("subject = %q, serial = %s (want %s)", cert.Subject.CommonName, cert.SerialNumber.Text(16), serial)
	}
	if len(cert.ExtKeyUsage) != 1 || cert.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth || cert.IsCA {
		t.Errorf("usage = %v, IsCA = %v", cert.ExtKeyUsage, cert.IsCA)
	}
}

func TestSignClientRejectsBadRequests(t *testing.T) {
	ca, _ := LoadOrCreate(t.TempDir(), time.Now())
	if _, _, err := ca.SignClient([]byte("garbage"), "a", time.Now()); err == nil {
		t.Error("garbage must fail")
	}
	_, csr, _ := NewAgentKey()
	csr[len(csr)-1] ^= 0xff // break the signature
	if _, _, err := ca.SignClient(csr, "a", time.Now()); err == nil {
		t.Error("a request with a broken signature must fail")
	}
}

func TestPinnedVerification(t *testing.T) {
	ca, _ := LoadOrCreate(t.TempDir(), time.Now())
	other, _ := LoadOrCreate(t.TempDir(), time.Now())
	now := time.Now()
	good, _ := ca.ServerCertificate(now)
	evil, _ := other.ServerCertificate(now)

	if err := verifyPinned(ca.Fingerprint(), good.Certificate, now); err != nil {
		t.Errorf("the hub's own certificate: %v", err)
	}
	if err := verifyPinned(ca.Fingerprint(), evil.Certificate, now); err == nil {
		t.Error("a certificate from another CA must be refused")
	}
	// The pinned CA in the chain does not help a leaf it did not sign.
	forged := [][]byte{evil.Certificate[0], ca.CertDER()}
	if err := verifyPinned(ca.Fingerprint(), forged, now); err == nil {
		t.Error("a leaf not signed by the pinned CA must be refused")
	}
	if err := verifyPinned(ca.Fingerprint(), good.Certificate, now.Add(2*serverValidity)); err == nil {
		t.Error("an expired certificate must be refused")
	}
	if err := verifyPinned(ca.Fingerprint(), nil, now); err == nil {
		t.Error("no certificate must be refused")
	}
}

func TestMutualTLSHandshake(t *testing.T) {
	ca, _ := LoadOrCreate(t.TempDir(), time.Now())
	serverCert, _ := ca.ServerCertificate(time.Now())
	key, csr, _ := NewAgentKey()
	der, serial, _ := ca.SignClient(csr, "agent-1", time.Now())
	clientCert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", ca.ServerTLS(serverCert))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	got := make(chan Identity, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		tc := c.(*tls.Conn)
		if err := tc.Handshake(); err != nil {
			got <- Identity{}
			return
		}
		id, _ := ClientIdentity(tc.ConnectionState())
		got <- id
	}()

	cfg, err := PinnedClientTLS(ca.Fingerprint(), &clientCert)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := tls.Dial("tcp", ln.Addr().String(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if id := <-got; id.AgentID != "agent-1" || id.Serial != serial {
		t.Errorf("identity = %+v, want agent-1 / %s", id, serial)
	}

	// Without a certificate the handshake works but there is no identity.
	cfg, _ = PinnedClientTLS(ca.Fingerprint(), nil)
	go func() {
		c, _ := ln.Accept()
		defer func() { _ = c.Close() }()
		tc := c.(*tls.Conn)
		_ = tc.Handshake()
		id, _ := ClientIdentity(tc.ConnectionState())
		got <- id
	}()
	conn, err = tls.Dial("tcp", ln.Addr().String(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if id := <-got; id.AgentID != "" {
		t.Errorf("anonymous client got identity %+v", id)
	}

	// A hub with another CA is not trusted.
	wrong, _ := LoadOrCreate(t.TempDir(), time.Now())
	cfg, _ = PinnedClientTLS(wrong.Fingerprint(), nil)
	go func() {
		c, _ := ln.Accept()
		if c != nil {
			_ = c.(*tls.Conn).Handshake()
			_ = c.Close()
		}
	}()
	if c, err := tls.Dial("tcp", ln.Addr().String(), cfg); err == nil {
		_ = c.Close()
		t.Error("connected to a hub whose CA is not pinned")
	}
}
