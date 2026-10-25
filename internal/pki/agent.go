package pki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"time"
)

// NewAgentKey makes the key pair of an agent and the certificate request to
// send to the hub. The key stays on the agent.
func NewAgentKey() (*ecdsa.PrivateKey, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "regulus-agent"},
	}, key)
	if err != nil {
		return nil, nil, err
	}
	return key, csr, nil
}

// PinnedClientTLS is the TLS configuration of an agent. It trusts a hub if the
// certificate it presents is signed by a CA with the given fingerprint, and
// ignores the host name: the pinned CA only signs the certificates of one hub,
// so the name adds nothing, and the hub may be reached by any address. cert may
// be nil, which is the case for enrollment.
func PinnedClientTLS(fingerprint string, cert *tls.Certificate) (*tls.Config, error) {
	pin, err := ParseFingerprint(fingerprint)
	if err != nil {
		return nil, err
	}
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS13,
		// The standard check needs a matching host name; the pinned check below
		// replaces it and is not optional.
		InsecureSkipVerify:    true, //nolint:gosec // see above
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error { return verifyPinned(pin, raw, time.Now()) },
	}
	if cert != nil {
		cfg.Certificates = []tls.Certificate{*cert}
	}
	return cfg, nil
}

// verifyPinned checks that the chain sent by the hub contains a CA whose
// fingerprint is pin and that the first certificate is a valid server
// certificate signed by it.
func verifyPinned(pin string, raw [][]byte, now time.Time) error {
	if len(raw) == 0 {
		return errors.New("the hub sent no certificate")
	}
	certs := make([]*x509.Certificate, len(raw))
	for i, der := range raw {
		c, err := x509.ParseCertificate(der)
		if err != nil {
			return fmt.Errorf("parse certificate: %w", err)
		}
		certs[i] = c
	}
	roots := x509.NewCertPool()
	found := false
	for _, c := range certs[1:] {
		if Fingerprint(c.Raw) == pin {
			roots.AddCert(c)
			found = true
		}
	}
	if !found {
		return errors.New("the hub is not the one this agent was enrolled with: its CA does not match the pinned fingerprint")
	}
	_, err := certs[0].Verify(x509.VerifyOptions{
		Roots:       roots,
		CurrentTime: now,
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
	return err
}
