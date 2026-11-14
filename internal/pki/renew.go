package pki

import (
	"crypto/tls"
	"crypto/x509"
	"sync"
	"time"
)

// serverRenewBefore is how long before it expires the listener certificate is
// replaced. Agents refuse an expired certificate, and a hub that is not
// restarted for a year (update mode "manual", no new release) would otherwise
// lock every agent out without a word.
const serverRenewBefore = 30 * 24 * time.Hour

// renewingServerCert hands out the certificate of the agent listener and
// issues a new one when the current one is about to expire.
type renewingServerCert struct {
	ca    *CA
	now   func() time.Time
	hosts []string
	// renewed is told when a certificate was replaced; may be nil.
	renewed func(notAfter time.Time)

	mu   sync.Mutex
	cert tls.Certificate
	end  time.Time
}

func (r *renewingServerCert) get(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	if len(r.cert.Certificate) == 0 || !now.Add(serverRenewBefore).Before(r.end) {
		first := len(r.cert.Certificate) == 0
		cert, err := r.ca.ServerCertificate(now, r.hosts...)
		if err != nil {
			if first || !now.Before(r.end) {
				return nil, err
			}
			return &r.cert, nil // the old one still works: try again at the next handshake
		}
		leaf, err := x509.ParseCertificate(cert.Certificate[0])
		if err != nil {
			return nil, err
		}
		r.cert, r.end = cert, leaf.NotAfter
		if !first && r.renewed != nil {
			r.renewed(r.end)
		}
	}
	return &r.cert, nil
}

// ServerTLSRenewing is ServerTLS for a listener that runs for a long time: the
// certificate is made when the first client connects and replaced
// serverRenewBefore its end. now is the clock (time.Now in production);
// renewed, if not nil, is called with the end of each replacement certificate
// so that the hub can log it. It returns the configuration and the first
// certificate's end.
func (ca *CA) ServerTLSRenewing(now func() time.Time, renewed func(notAfter time.Time), hosts ...string) (*tls.Config, time.Time, error) {
	r := &renewingServerCert{ca: ca, now: now, hosts: hosts, renewed: renewed}
	if _, err := r.get(nil); err != nil { // fail at start, not at the first agent
		return nil, time.Time{}, err
	}
	cfg := ca.ServerTLS(tls.Certificate{})
	cfg.Certificates = nil
	cfg.GetCertificate = r.get
	return cfg, r.end, nil
}
