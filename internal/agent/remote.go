package agent

import (
	"context"
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	agentv1 "github.com/CodexCircle/Regulus/internal/gen/regulus/agent/v1"
	"github.com/CodexCircle/Regulus/internal/pki"
)

const enrollTimeout = 30 * time.Second

// File names inside the agent's data directory.
const (
	keyFile  = "agent.key"
	certFile = "agent.crt"
	caFile   = "ca.crt"
)

// ErrNotEnrolled is returned by LoadCredentials when the agent has no
// credentials yet.
var ErrNotEnrolled = errors.New("the agent is not enrolled")

// Credentials identify an enrolled agent to the hub and tell it which hub to
// trust.
type Credentials struct {
	AgentID string
	Key     *ecdsa.PrivateKey
	CertDER []byte
	CADER   []byte
}

// LoadCredentials reads the credentials saved in dir by Enroll.
func LoadCredentials(dir string) (*Credentials, error) {
	keyPEM, err := os.ReadFile(filepath.Join(dir, keyFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotEnrolled
	}
	if err != nil {
		return nil, err
	}
	certPEM, err := os.ReadFile(filepath.Join(dir, certFile))
	if err != nil {
		return nil, fmt.Errorf("read agent certificate: %w", err)
	}
	caPEM, err := os.ReadFile(filepath.Join(dir, caFile))
	if err != nil {
		return nil, fmt.Errorf("read CA certificate: %w", err)
	}
	keyBlock, _ := pem.Decode(keyPEM)
	certBlock, _ := pem.Decode(certPEM)
	caBlock, _ := pem.Decode(caPEM)
	if keyBlock == nil || certBlock == nil || caBlock == nil {
		return nil, errors.New("the agent credentials in " + dir + " are damaged: remove them and enroll again")
	}
	key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse agent key: %w", err)
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse agent certificate: %w", err)
	}
	return &Credentials{AgentID: cert.Subject.CommonName, Key: key, CertDER: certBlock.Bytes, CADER: caBlock.Bytes}, nil
}

func (c *Credentials) save(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	keyDER, err := x509.MarshalECPrivateKey(c.Key)
	if err != nil {
		return err
	}
	// The certificate comes last: its presence marks a complete enrollment.
	if err := os.WriteFile(filepath.Join(dir, keyFile), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, caFile), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.CADER}), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, certFile), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.CertDER}), 0o644)
}

// Enroll registers the agent with the hub at hubAddr ("host:port") using a
// token from the dashboard, and saves the credentials in dir. fingerprint is
// the fingerprint of the hub's CA, which the agent checks before it sends the
// token: without it, anyone who can intercept the connection could take it.
func Enroll(ctx context.Context, hubAddr, fingerprint, token, version, dir string) (*Credentials, error) {
	tlsCfg, err := pki.PinnedClientTLS(fingerprint, nil)
	if err != nil {
		return nil, err
	}
	conn, err := grpc.NewClient(hubAddr, grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)))
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()

	key, csr, err := pki.NewAgentKey()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, enrollTimeout)
	defer cancel()
	res, err := agentv1.NewEnrollmentServiceClient(conn).Enroll(ctx, &agentv1.EnrollRequest{
		Token:        token,
		CsrDer:       csr,
		System:       systemInfo(),
		AgentVersion: version,
	})
	if err != nil {
		return nil, fmt.Errorf("enroll with the hub: %w", err)
	}

	want, _ := pki.ParseFingerprint(fingerprint)
	if pki.Fingerprint(res.GetCaCertificateDer()) != want {
		return nil, errors.New("the hub answered with a CA that does not match the fingerprint")
	}
	cert, err := x509.ParseCertificate(res.GetCertificateDer())
	if err != nil || cert.Subject.CommonName != res.GetAgentId() {
		return nil, errors.New("the hub answered with an invalid certificate")
	}
	creds := &Credentials{AgentID: res.GetAgentId(), Key: key, CertDER: res.GetCertificateDer(), CADER: res.GetCaCertificateDer()}
	if err := creds.save(dir); err != nil {
		return nil, fmt.Errorf("save the agent credentials: %w", err)
	}
	return creds, nil
}

// Dial opens the connection to the hub at hubAddr with the credentials. The
// connection is made lazily and re-made by gRPC when it drops.
func (c *Credentials) Dial(hubAddr string) (*grpc.ClientConn, error) {
	tlsCfg, err := pki.PinnedClientTLS(pki.Fingerprint(c.CADER), &tls.Certificate{
		Certificate: [][]byte{c.CertDER},
		PrivateKey:  c.Key,
	})
	if err != nil {
		return nil, err
	}
	return grpc.NewClient(hubAddr, grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)))
}
