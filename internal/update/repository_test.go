package update

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The repository moved once (CodexCircle to Stellatinus) and the signature
// identity, the installer and the links kept the old name. These tests fail
// when a name is left behind after the next move.

var textFile = regexp.MustCompile(`\.(go|sh|md|ya?ml|tsx?|json|service|path|timer)$|^(Makefile|LICENSE)$`)

// goModule is the Go module path. It is kept on purpose (see
// docs/decisions.md): it is an import path, not an address anyone downloads
// from.
const goModule = "github.com/CodexCircle/Regulus"

func TestNoStaleRepositoryName(t *testing.T) {
	root := filepath.Join("..", "..")
	skip := map[string]bool{
		filepath.Join("internal", "update", "signature_test.go"):  true, // checks the old identity is refused
		filepath.Join("internal", "update", "repository_test.go"): true,
		filepath.Join("docs", "decisions.md"):                     true, // tells the story of the move
	}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		if skip[rel] || rel == "go.mod" || rel == "go.sum" || strings.HasSuffix(rel, ".pb.go") ||
			filepath.Base(rel) == "package-lock.json" || !textFile.MatchString(d.Name()) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(data), "\n") {
			if !strings.Contains(line, "CodexCircle/Regulus") {
				continue
			}
			if strings.Contains(line, goModule+"/internal") { // an import or the generated Go package option
				continue
			}
			t.Errorf("%s:%d names the old repository: %s", rel, i+1, strings.TrimSpace(line))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestInstallerUsesTheReleaseRepository(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "packaging", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "\nREPO=" + Repository + "\n"; !strings.Contains(string(data), want) {
		t.Errorf("packaging/install.sh must contain %q", strings.TrimSpace(want))
	}
}

func TestSignerIdentityFollowsRepository(t *testing.T) {
	want := "https://github.com/" + Repository + "/.github/workflows/release.yml@refs/tags/"
	if signerWorkflow != want {
		t.Errorf("signerWorkflow = %q, want %q", signerWorkflow, want)
	}
}
