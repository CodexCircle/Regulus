// Package pki is the small certificate authority of the hub. It signs the
// client certificates of enrolled agents and the certificate the hub shows
// them, and gives the agents what they need to trust the hub without a public
// certificate: they pin the fingerprint of the CA. agent.go holds what the
// agent side needs (its key and certificate request). Which agents exist, their
// tokens and their revocation are in package fleet.
package pki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	caValidity     = 20 * 365 * 24 * time.Hour
	serverValidity = 365 * 24 * time.Hour
	// ClientValidity is how long the certificate of an agent is valid. There is
	// no renewal yet, so it is long; revocation does not depend on it.
	ClientValidity = 5 * 365 * 24 * time.Hour

	clockSkew = 5 * time.Minute
)

// CA is the hub's certificate authority.
type CA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

// LoadOrCreate loads the CA stored in dir (ca.crt and ca.key), or creates it
// when neither file exists. The key is readable by the owner only.
func LoadOrCreate(dir string, now time.Time) (*CA, error) {
	certPath, keyPath := filepath.Join(dir, "ca.crt"), filepath.Join(dir, "ca.key")
	certPEM, certErr := os.ReadFile(certPath)
	keyPEM, keyErr := os.ReadFile(keyPath)
	switch {
	case certErr == nil && keyErr == nil:
		return parseCA(certPEM, keyPEM)
	case errors.Is(certErr, os.ErrNotExist) && errors.Is(keyErr, os.ErrNotExist):
		return createCA(dir, certPath, keyPath, now)
	default:
		return nil, fmt.Errorf("incomplete CA in %s: need both ca.crt and ca.key (%v, %v)", dir, certErr, keyErr)
	}
}

func parseCA(certPEM, keyPEM []byte) (*CA, error) {
	certBlock, _ := pem.Decode(certPEM)
	keyBlock, _ := pem.Decode(keyPEM)
	if certBlock == nil || keyBlock == nil {
		return nil, errors.New("the CA files are not PEM")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse CA certificate: %w", err)
	}
	key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse CA key: %w", err)
	}
	return &CA{cert: cert, key: key}, nil
}

func createCA(dir, certPath, keyPath string, now time.Time) (*CA, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := newSerial()
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Regulus hub CA"},
		NotBefore:             now.Add(-clockSkew),
		NotAfter:              now.Add(caValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	// The key first: a certificate without its key would leave a half CA.
	if err := writeFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return nil, err
	}
	if err := writeFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	return &CA{cert: cert, key: key}, nil
}

func writeFile(path string, data []byte, mode os.FileMode) error {
	return os.WriteFile(path, data, mode)
}

func newSerial() (*big.Int, error) {
	return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
}

// CertDER is the certificate of the CA.
func (ca *CA) CertDER() []byte { return ca.cert.Raw }

// Fingerprint is what an agent pins to trust this hub, as "sha256:" followed by
// 64 hex digits.
func (ca *CA) Fingerprint() string { return Fingerprint(ca.cert.Raw) }

// Fingerprint returns "sha256:" and the hex SHA-256 of a DER certificate.
func Fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ParseFingerprint accepts a fingerprint written as "sha256:<hex>" or plain
// hex, with optional colons, in any case, and returns the canonical form.
func ParseFingerprint(s string) (string, error) {
	h := strings.ToLower(strings.TrimSpace(s))
	h = strings.TrimPrefix(h, "sha256:")
	h = strings.ReplaceAll(h, ":", "")
	if len(h) != sha256.Size*2 {
		return "", errors.New("a CA fingerprint is 64 hexadecimal digits (sha256)")
	}
	if _, err := hex.DecodeString(h); err != nil {
		return "", errors.New("a CA fingerprint is 64 hexadecimal digits (sha256)")
	}
	return "sha256:" + h, nil
}

// ServerCertificate issues a fresh certificate for the hub's agent listener,
// valid for the given host names or addresses. It is not stored: a new one is
// made at every start. The chain includes the CA, so agents that only know the
// fingerprint can find what they pin.
func (ca *CA) ServerCertificate(now time.Time, hosts ...string) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := newSerial()
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "Regulus hub"},
		NotBefore:    now.Add(-clockSkew),
		NotAfter:     now.Add(serverValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
	}
	tmpl.DNSNames = append(tmpl.DNSNames, hosts...)
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der, ca.cert.Raw}, PrivateKey: key}, nil
}

// SignClient issues the client certificate of an agent from a certificate
// request. The subject of the request is ignored; the certificate is made for
// agentID. It returns the certificate (DER) and its serial number in hex.
func (ca *CA) SignClient(csrDER []byte, agentID string, now time.Time) (certDER []byte, serialHex string, err error) {
	csr, err := x509.ParseCertificateRequest(csrDER)
	if err != nil {
		return nil, "", fmt.Errorf("parse certificate request: %w", err)
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, "", fmt.Errorf("certificate request signature: %w", err)
	}
	switch pub := csr.PublicKey.(type) {
	case *ecdsa.PublicKey:
		if pub.Curve != elliptic.P256() && pub.Curve != elliptic.P384() {
			return nil, "", errors.New("use an ECDSA P-256 or P-384 key")
		}
	default:
		return nil, "", errors.New("use an ECDSA P-256 or P-384 key")
	}
	serial, err := newSerial()
	if err != nil {
		return nil, "", err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: agentID, Organization: []string{"Regulus agent"}},
		NotBefore:    now.Add(-clockSkew),
		NotAfter:     now.Add(ClientValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, csr.PublicKey, ca.key)
	if err != nil {
		return nil, "", err
	}
	return der, serial.Text(16), nil
}

// ServerTLS is the TLS configuration of the agent listener. A client
// certificate is not required, because enrollment happens before an agent has
// one, but a certificate that is presented must be signed by this CA. The
// services decide which calls need an identity (see ClientIdentity).
func (ca *CA) ServerTLS(cert tls.Certificate) *tls.Config {
	pool := x509.NewCertPool()
	pool.AddCert(ca.cert)
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.VerifyClientCertIfGiven,
		ClientCAs:    pool,
	}
}

// Identity is a verified client certificate.
type Identity struct {
	AgentID string
	Serial  string // hex
}

// ClientIdentity returns the identity from the state of a TLS connection, or
// false if the client presented no verified certificate.
func ClientIdentity(state tls.ConnectionState) (Identity, bool) {
	if len(state.VerifiedChains) == 0 || len(state.VerifiedChains[0]) == 0 {
		return Identity{}, false
	}
	leaf := state.VerifiedChains[0][0]
	if leaf.Subject.CommonName == "" {
		return Identity{}, false
	}
	return Identity{AgentID: leaf.Subject.CommonName, Serial: leaf.SerialNumber.Text(16)}, true
}
