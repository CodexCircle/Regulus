package update

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/testing/ca"
)

const releaseSigner = "https://github.com/CodexCircle/Regulus/.github/workflows/release.yml@refs/tags/v0.2.0"

var checksumsText = []byte("abc  regulus_0.2.0_linux_amd64.tar.gz\n")

func newVirtual(t *testing.T) *ca.VirtualSigstore {
	t.Helper()
	v, err := ca.NewVirtualSigstore()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func testVerifier() *SigstoreVerifier { return &SigstoreVerifier{withoutSCT: true} }

func TestSignatureOfTheReleaseWorkflowIsAccepted(t *testing.T) {
	v := newVirtual(t)
	entity, err := v.Sign(releaseSigner, signerIssuer, checksumsText)
	if err != nil {
		t.Fatal(err)
	}
	if err := testVerifier().verifyEntity(v, entity, "v0.2.0", checksumsText); err != nil {
		t.Fatalf("a valid signature was refused: %v", err)
	}
}

func TestSignatureRefusals(t *testing.T) {
	v := newVirtual(t)
	other := newVirtual(t) // a different Sigstore: its keys are not trusted
	sign := func(sigstore *ca.VirtualSigstore, identity, issuer string, data []byte) *ca.TestEntity {
		t.Helper()
		e, err := sigstore.Sign(identity, issuer, data)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	for name, tc := range map[string]struct {
		entity    *ca.TestEntity
		tag       string
		checksums []byte
	}{
		"the checksums were changed": {sign(v, releaseSigner, signerIssuer, checksumsText), "v0.2.0", []byte("evil  regulus_0.2.0_linux_amd64.tar.gz\n")},
		"signed for another tag":     {sign(v, strings.Replace(releaseSigner, "v0.2.0", "v0.1.0", 1), signerIssuer, checksumsText), "v0.2.0", checksumsText},
		"another workflow":           {sign(v, "https://github.com/CodexCircle/Regulus/.github/workflows/ci.yml@refs/tags/v0.2.0", signerIssuer, checksumsText), "v0.2.0", checksumsText},
		"another repository":         {sign(v, "https://github.com/evil/Regulus/.github/workflows/release.yml@refs/tags/v0.2.0", signerIssuer, checksumsText), "v0.2.0", checksumsText},
		"another issuer":             {sign(v, releaseSigner, "https://accounts.example.com", checksumsText), "v0.2.0", checksumsText},
		"an untrusted Sigstore":      {sign(other, releaseSigner, signerIssuer, checksumsText), "v0.2.0", checksumsText},
	} {
		if err := testVerifier().verifyEntity(v, tc.entity, tc.tag, tc.checksums); err == nil {
			t.Errorf("%s: the signature was accepted", name)
		}
	}
}

func TestVerifyRejectsAFileThatIsNotABundle(t *testing.T) {
	trust := func(context.Context) (root.TrustedMaterial, error) { return newVirtual(t), nil }
	err := (&SigstoreVerifier{Trust: trust}).Verify(t.Context(), "v0.2.0", checksumsText, []byte("not json"))
	if err == nil || !strings.Contains(err.Error(), "not a Sigstore bundle") {
		t.Fatalf("error = %v", err)
	}
}

func TestVerifyReportsTrustFailures(t *testing.T) {
	trust := func(context.Context) (root.TrustedMaterial, error) { return nil, errors.New("no trust") }
	bundle := []byte(`{"mediaType":"application/vnd.dev.sigstore.bundle.v0.3+json","verificationMaterial":{},"messageSignature":{}}`)
	err := (&SigstoreVerifier{Trust: trust}).Verify(t.Context(), "v0.2.0", checksumsText, bundle)
	if err == nil {
		t.Fatal("verification passed without trust")
	}
}

func TestTheEmbeddedTrustedRootLoads(t *testing.T) {
	tr, err := root.NewTrustedRootFromJSON(trustedRootJSON)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.FulcioCertificateAuthorities()) == 0 || len(tr.RekorLogs()) == 0 || len(tr.CTLogs()) == 0 {
		t.Errorf("the snapshot lacks Fulcio, Rekor or CT material: %d, %d, %d", len(tr.FulcioCertificateAuthorities()), len(tr.RekorLogs()), len(tr.CTLogs()))
	}
}
