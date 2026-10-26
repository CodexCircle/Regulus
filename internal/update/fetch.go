package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Names of the files of a release, for Fetcher.
const (
	FileChecksums = checksumsName
	FileBundle    = bundleName
)

// Fetcher writes one file of a release to w, from wherever the release is
// offered. file is FileChecksums, FileBundle or the name of the archive.
type Fetcher func(ctx context.Context, file string, w io.Writer) error

// limitWriter fails once more than n bytes are written.
type limitWriter struct {
	w io.Writer
	n int64
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > l.n {
		return 0, errors.New("the file is larger than any release should be")
	}
	l.n -= int64(len(p))
	return l.w.Write(p)
}

// StageFetched keeps a release that was offered by a hub rather than found on
// GitHub: the files are fetched, the archive is compared with the checksum list
// (and with wantSHA, the value the hub named), and the signature of the list is
// verified when the Stager has a Verifier. The result is laid out like Stage's,
// so the helper that runs as root installs it the same way.
func (s *Stager) StageFetched(ctx context.Context, v Version, plat Platform, wantSHA string, fetch Fetcher) (Staged, error) {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return Staged{}, err
	}
	work, err := os.MkdirTemp(s.Dir, ".stage-")
	if err != nil {
		return Staged{}, err
	}
	defer func() { _ = os.RemoveAll(work) }()

	small := func(name string) ([]byte, error) {
		var buf bytes.Buffer
		if err := fetch(ctx, name, &limitWriter{w: &buf, n: maxSmallFile}); err != nil {
			return nil, fmt.Errorf("fetch %s: %w", name, err)
		}
		return buf.Bytes(), nil
	}
	sumsData, err := small(checksumsName)
	if err != nil {
		return Staged{}, err
	}
	bundleData, err := small(bundleName)
	if err != nil {
		return Staged{}, err
	}
	tag := "v" + v.String()
	verified := VerifiedChecksum
	if s.Verifier != nil {
		if err := s.Verifier.Verify(ctx, tag, sumsData, bundleData); err != nil {
			return Staged{}, err
		}
		verified = VerifiedSignature
	}

	name := ArchiveName(v, plat.OS, plat.Arch)
	want, err := checksumFor(sumsData, name)
	if err != nil {
		return Staged{}, err
	}
	if wantSHA != "" && subtle.ConstantTimeCompare([]byte(wantSHA), []byte(want)) != 1 {
		return Staged{}, fmt.Errorf("the hub offered a checksum for %s that is not the one in the checksum list", name)
	}
	f, err := os.OpenFile(filepath.Join(work, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return Staged{}, err
	}
	h := sha256.New()
	cw := &countWriter{w: io.MultiWriter(f, h)}
	err = fetch(ctx, name, &limitWriter{w: cw, n: maxArchive})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return Staged{}, fmt.Errorf("fetch %s: %w", name, err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		return Staged{}, fmt.Errorf("%s does not match its checksum (expected %s, got %s)", name, want, got)
	}

	for file, data := range map[string][]byte{checksumsName: sumsData, bundleName: bundleData} {
		if err := os.WriteFile(filepath.Join(work, file), data, 0o600); err != nil {
			return Staged{}, err
		}
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	st := Staged{Version: v.String(), Tag: tag, Archive: name, SHA256: got, Size: cw.n, Verified: verified, StagedAt: now().UTC(), Signed: true}
	meta, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return Staged{}, err
	}
	if err := os.WriteFile(filepath.Join(work, metaName), meta, 0o600); err != nil {
		return Staged{}, err
	}
	return s.install(work, st)
}

type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// Open opens a file of a staged release so it can be served to an agent. The
// name must be the checksum list, the signature bundle or an archive of that
// release; nothing else in the cache is reachable.
func (s *Stager) Open(version, name string) (*os.File, int64, error) {
	v, err := ParseVersion(version)
	if err != nil || v.String() != version {
		return nil, 0, errors.New("invalid version")
	}
	ok := name == checksumsName || name == bundleName
	for _, p := range servedPlatforms {
		ok = ok || name == ArchiveName(v, p.OS, p.Arch)
	}
	if !ok {
		return nil, 0, errors.New("no such file in a release")
	}
	f, err := openRegular(filepath.Join(s.Dir, version, name))
	if err != nil {
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, 0, err
	}
	return f, info.Size(), nil
}

// servedPlatforms are the builds a release has (see .goreleaser.yaml).
var servedPlatforms = []Platform{{"linux", "amd64"}, {"linux", "arm64"}}

// HasArchives reports whether the staged release of a version holds the
// archive of every platform given, and has its signature bundle.
func (s *Stager) HasArchives(version string, plats ...Platform) bool {
	v, err := ParseVersion(version)
	if err != nil {
		return false
	}
	data, err := readRegular(filepath.Join(s.Dir, version, metaName), maxControlFile)
	if err != nil {
		return false
	}
	var st Staged
	if json.Unmarshal(data, &st) != nil || st.Version != v.String() || st.Verified != VerifiedSignature {
		return false
	}
	for _, p := range append([]Platform{{}}, plats...) {
		name := bundleName
		if p != (Platform{}) {
			name = ArchiveName(v, p.OS, p.Arch)
		}
		f, err := openRegular(filepath.Join(s.Dir, version, name))
		if err != nil {
			return false
		}
		_ = f.Close()
	}
	return true
}

// Offer describes the archive of a platform in the staged release of a
// version, from the signed checksum list: its name, SHA-256 and size.
func (s *Stager) Offer(version string, p Platform) (name, sha string, size int64, err error) {
	v, err := ParseVersion(version)
	if err != nil {
		return "", "", 0, err
	}
	name = ArchiveName(v, p.OS, p.Arch)
	sums, err := readRegular(filepath.Join(s.Dir, version, checksumsName), maxSmallFile)
	if err != nil {
		return "", "", 0, err
	}
	if sha, err = checksumFor(sums, name); err != nil {
		return "", "", 0, err
	}
	f, err := openRegular(filepath.Join(s.Dir, version, name))
	if err != nil {
		return "", "", 0, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return "", "", 0, err
	}
	return name, sha, info.Size(), nil
}
