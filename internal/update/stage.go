package update

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// How far a staged release was checked.
const (
	// VerifiedChecksum: the archive matches the SHA-256 in the release's
	// checksums.txt. That catches a corrupted or truncated download, but both
	// files come from the same place, so it does not prove who made them.
	VerifiedChecksum = "checksum"
)

// Names of the files of a release (see .goreleaser.yaml).
const (
	checksumsName = "checksums.txt"
	bundleName    = "checksums.txt.sigstore.json"
	metaName      = "staged.json"
)

const (
	maxSmallFile = 1 << 20   // checksums.txt and the signature bundle
	maxArchive   = 512 << 20 // far above any release, but a bound
)

// ArchiveName is the name GoReleaser gives the archive of a platform.
func ArchiveName(v Version, goos, goarch string) string {
	return fmt.Sprintf("regulus_%s_%s_%s.tar.gz", v, goos, goarch)
}

// Staged describes a downloaded release kept in the cache.
type Staged struct {
	Version  string    `json:"version"`
	Tag      string    `json:"tag"`
	Archive  string    `json:"archive"`
	SHA256   string    `json:"sha256"`
	Size     int64     `json:"size"`
	Verified string    `json:"verified"`
	StagedAt time.Time `json:"staged_at"`
	// Signed is true when the signature bundle of checksums.txt was published
	// and kept next to it, for whoever applies the update to check.
	Signed bool `json:"signed"`
}

// Path returns where a file of the staged release is, under the cache dir.
func (s Staged) Path(dir, name string) string {
	return filepath.Join(dir, s.Version, name)
}

// Stager downloads releases into a cache directory.
type Stager struct {
	// Dir is the cache; it is created when needed.
	Dir    string
	Client *http.Client
	Now    func() time.Time
}

func (s *Stager) client() *http.Client {
	c := s.Client
	if c == nil {
		c = http.DefaultClient
	}
	// Never follow a redirect to a plain-text address, whatever the client.
	cc := *c
	prev := cc.CheckRedirect
	cc.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 0 && via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return errors.New("refusing a redirect from https to plain http")
		}
		if prev != nil {
			return prev(req, via)
		}
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		return nil
	}
	return &cc
}

// Stage downloads the archive of the release for a platform, checks it against
// the release's checksums.txt and keeps it, with the checksums and the
// signature bundle, in Dir/<version>/. Any other staged version is removed.
// Nothing reaches the final place unless the checksum matches.
func (s *Stager) Stage(ctx context.Context, rel Release, goos, goarch string) (Staged, error) {
	archiveName := ArchiveName(rel.Version, goos, goarch)
	archive, ok := rel.Asset(archiveName)
	if !ok {
		return Staged{}, fmt.Errorf("release %s has no %s", rel.Tag, archiveName)
	}
	sums, ok := rel.Asset(checksumsName)
	if !ok {
		return Staged{}, fmt.Errorf("release %s has no %s, so the download cannot be checked", rel.Tag, checksumsName)
	}

	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return Staged{}, err
	}
	work, err := os.MkdirTemp(s.Dir, ".stage-")
	if err != nil {
		return Staged{}, err
	}
	defer func() { _ = os.RemoveAll(work) }()

	sumsData, err := s.fetchSmall(ctx, sums.URL)
	if err != nil {
		return Staged{}, fmt.Errorf("download %s: %w", checksumsName, err)
	}
	want, err := checksumFor(sumsData, archiveName)
	if err != nil {
		return Staged{}, err
	}

	got, size, err := s.fetchFile(ctx, archive.URL, filepath.Join(work, archiveName), maxArchive)
	if err != nil {
		return Staged{}, fmt.Errorf("download %s: %w", archiveName, err)
	}
	if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		return Staged{}, fmt.Errorf("%s does not match its checksum (expected %s, got %s): the download is corrupt or has been tampered with", archiveName, want, got)
	}
	if archive.Size > 0 && archive.Size != size {
		return Staged{}, fmt.Errorf("%s is %d bytes but the release says %d", archiveName, size, archive.Size)
	}
	if err := os.WriteFile(filepath.Join(work, checksumsName), sumsData, 0o600); err != nil {
		return Staged{}, err
	}

	st := Staged{Version: rel.Version.String(), Tag: rel.Tag, Archive: archiveName, SHA256: got, Size: size, Verified: VerifiedChecksum}
	if bundle, ok := rel.Asset(bundleName); ok {
		// The bundle is optional here: it is only kept. A release without it
		// can be downloaded but never counts as signed.
		if data, err := s.fetchSmall(ctx, bundle.URL); err == nil {
			if err := os.WriteFile(filepath.Join(work, bundleName), data, 0o600); err != nil {
				return Staged{}, err
			}
			st.Signed = true
		}
	}
	if s.Now != nil {
		st.StagedAt = s.Now().UTC()
	} else {
		st.StagedAt = time.Now().UTC()
	}
	meta, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return Staged{}, err
	}
	if err := os.WriteFile(filepath.Join(work, metaName), meta, 0o600); err != nil {
		return Staged{}, err
	}

	// The version is made of digits, letters, dots and hyphens only (it was
	// parsed), so it is a safe directory name.
	final := filepath.Join(s.Dir, st.Version)
	if err := os.RemoveAll(final); err != nil {
		return Staged{}, err
	}
	if err := os.Rename(work, final); err != nil {
		return Staged{}, err
	}
	s.prune(st.Version)
	return st, nil
}

// prune removes every staged version except keep, and leftovers of downloads
// that were interrupted.
func (s *Stager) prune(keep string) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.Name() != keep {
			_ = os.RemoveAll(filepath.Join(s.Dir, e.Name()))
		}
	}
}

// Current returns the staged release in the cache, if there is one and its
// files are still intact.
func (s *Stager) Current() (Staged, bool) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return Staged{}, false
	}
	var best Staged
	var bestV Version
	found := false
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.Dir, e.Name(), metaName))
		if err != nil {
			continue
		}
		var st Staged
		v, verr := ParseVersion(e.Name())
		if json.Unmarshal(data, &st) != nil || verr != nil || st.Version != v.String() || st.Version != e.Name() {
			continue
		}
		info, err := os.Stat(filepath.Join(s.Dir, st.Version, st.Archive))
		if err != nil || info.Size() != st.Size {
			continue
		}
		if !found || v.Compare(bestV) > 0 {
			best, bestV, found = st, v, true
		}
	}
	return best, found
}

// Verify hashes the staged archive again and compares it with the recorded
// checksum, so a file changed on disk since the download is noticed.
func (s *Stager) Verify(st Staged) error {
	f, err := os.Open(st.Path(s.Dir, st.Archive))
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(h.Sum(nil))), []byte(st.SHA256)) != 1 {
		return fmt.Errorf("%s changed after it was downloaded", st.Archive)
	}
	return nil
}

func (s *Stager) fetchSmall(ctx context.Context, rawURL string) ([]byte, error) {
	res, err := s.get(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxSmallFile+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSmallFile {
		return nil, errors.New("file is unexpectedly large")
	}
	return data, nil
}

// fetchFile downloads rawURL into path and returns the SHA-256 (hex) and size.
func (s *Stager) fetchFile(ctx context.Context, rawURL, path string, limit int64) (sum string, size int64, err error) {
	res, err := s.get(ctx, rawURL)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = res.Body.Close() }()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return "", 0, err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(res.Body, limit+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", 0, err
	}
	if n > limit {
		return "", 0, errors.New("file is larger than any release should be")
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func (s *Stager) get(ctx context.Context, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	res, err := s.client().Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		_ = res.Body.Close()
		return nil, fmt.Errorf("the server answered %s", res.Status)
	}
	return res, nil
}

// checksumFor finds the SHA-256 (lower-case hex) of a file in the text of a
// checksums.txt, whose lines look like "<hash>  <name>".
func checksumFor(sums []byte, name string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		hash, file, ok := strings.Cut(strings.TrimSpace(sc.Text()), " ")
		if !ok {
			continue
		}
		file = strings.TrimPrefix(strings.TrimSpace(file), "*") // binary-mode marker
		if file != name {
			continue
		}
		hash = strings.ToLower(hash)
		if len(hash) != sha256.Size*2 {
			return "", fmt.Errorf("%s lists a malformed checksum for %s", checksumsName, name)
		}
		if _, err := hex.DecodeString(hash); err != nil {
			return "", fmt.Errorf("%s lists a malformed checksum for %s", checksumsName, name)
		}
		return hash, nil
	}
	return "", fmt.Errorf("%s does not list %s", checksumsName, name)
}
