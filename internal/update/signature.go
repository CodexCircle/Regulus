package update

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
	"github.com/sigstore/sigstore-go/pkg/verify"
)

// VerifiedSignature: the checksums file carries a valid Sigstore signature
// made by the release workflow of this repository for this very tag, and the
// archive matches it.
const VerifiedSignature = "signature"

// The identity that signs releases: the GitHub Actions run of the release
// workflow, at the tag of the release (see .goreleaser.yaml and release.yml).
const (
	signerIssuer   = "https://token.actions.githubusercontent.com"
	signerWorkflow = "https://github.com/CodexCircle/Regulus/.github/workflows/release.yml@refs/tags/"
)

// Verifier checks the signature of a release's checksums.txt.
type Verifier interface {
	// Verify checks the Sigstore bundle of checksums for the release at tag.
	Verify(ctx context.Context, tag string, checksums, bundleJSON []byte) error
}

// trustedRootJSON is a snapshot of the public Sigstore trusted root, used when
// the up-to-date one cannot be fetched. Refresh it with `go generate`-style
// steps described in the README when Sigstore rotates its keys; a stale
// snapshot only makes verification fail, never pass.
//
//go:embed trusted_root.json
var trustedRootJSON []byte

// TrustSource provides the Sigstore trusted material.
type TrustSource func(ctx context.Context) (root.TrustedMaterial, error)

// SigstoreVerifier verifies bundles made by cosign's keyless signing.
type SigstoreVerifier struct {
	// Trust provides the trusted root; nil uses PublicTrust with no cache.
	Trust TrustSource
	// Signer is the expected identity (the certificate's subject alternative
	// name) for a tag; nil expects the release workflow of this repository.
	Signer func(tag string) string
	// Issuer is the expected OIDC issuer; empty means GitHub Actions.
	Issuer string

	// withoutSCT drops the certificate timestamp requirement; the virtual
	// Sigstore of the tests does not issue them.
	withoutSCT bool
}

// Verify implements Verifier.
func (v *SigstoreVerifier) Verify(ctx context.Context, tag string, checksums, bundleJSON []byte) error {
	var b bundle.Bundle
	if err := b.UnmarshalJSON(bundleJSON); err != nil {
		return fmt.Errorf("the signature file is not a Sigstore bundle: %w", err)
	}
	trust := v.Trust
	if trust == nil {
		trust = PublicTrust("")
	}
	material, err := trust(ctx)
	if err != nil {
		return err
	}
	return v.verifyEntity(material, &b, tag, checksums)
}

func (v *SigstoreVerifier) verifyEntity(material root.TrustedMaterial, entity verify.SignedEntity, tag string, checksums []byte) error {
	opts := []verify.VerifierOption{verify.WithTransparencyLog(1), verify.WithObserverTimestamps(1)}
	if !v.withoutSCT {
		opts = append(opts, verify.WithSignedCertificateTimestamps(1))
	}
	verifier, err := verify.NewVerifier(material, opts...)
	if err != nil {
		return fmt.Errorf("signature verifier: %w", err)
	}
	signer := signerWorkflow + tag
	if v.Signer != nil {
		signer = v.Signer(tag)
	}
	issuer := v.Issuer
	if issuer == "" {
		issuer = signerIssuer
	}
	identity, err := verify.NewShortCertificateIdentity(issuer, "", signer, "")
	if err != nil {
		return err
	}
	policy := verify.NewPolicy(verify.WithArtifact(bytes.NewReader(checksums)), verify.WithCertificateIdentity(identity))
	if _, err := verifier.Verify(entity, policy); err != nil {
		return fmt.Errorf("the signature of the release checksums is not valid for %s: %w", signer, err)
	}
	return nil
}

const (
	trustRefreshEvery = 12 * time.Hour
	trustFetchTimeout = 20 * time.Second
)

// PublicTrust returns the trust source for the public Sigstore instance. It
// asks the Sigstore TUF repository for the current trusted root (so a key
// rotation reaches a hub that nobody updated), keeping the answer in cacheDir
// (empty: not cached on disk), and falls back to the snapshot embedded in the
// binary when the repository cannot be reached.
func PublicTrust(cacheDir string) TrustSource {
	var (
		mu      sync.Mutex
		current root.TrustedMaterial
		fetched time.Time
	)
	return func(ctx context.Context) (root.TrustedMaterial, error) {
		mu.Lock()
		defer mu.Unlock()
		if current != nil && time.Since(fetched) < trustRefreshEvery {
			return current, nil
		}
		tr, err := fetchTrustedRoot(ctx, cacheDir)
		if err != nil {
			if current != nil {
				return current, nil // keep the last good one
			}
			tr, err = root.NewTrustedRootFromJSON(trustedRootJSON)
			if err != nil {
				return nil, fmt.Errorf("embedded Sigstore trusted root: %w", err)
			}
		}
		current, fetched = tr, time.Now()
		return tr, nil
	}
}

func fetchTrustedRoot(ctx context.Context, cacheDir string) (*root.TrustedRoot, error) {
	opts := tuf.DefaultOptions()
	if cacheDir != "" {
		opts = opts.WithCachePath(cacheDir)
	} else {
		opts = opts.WithDisableLocalCache()
	}
	type result struct {
		tr  *root.TrustedRoot
		err error
	}
	done := make(chan result, 1)
	go func() {
		tr, err := root.FetchTrustedRootWithOptions(opts)
		done <- result{tr, err}
	}()
	select {
	case r := <-done:
		return r.tr, r.err
	case <-time.After(trustFetchTimeout):
		return nil, errors.New("timeout fetching the Sigstore trusted root")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
