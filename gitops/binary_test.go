package gitops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// withFakeGitOnPath prepends a directory holding a copy of the fakegit
// binary, named exactly "git" (or "git.exe" on Windows), to PATH — so
// LocateNamed(ctx, "git") (and Locate) resolve to it via ordinary PATH
// lookup, exactly like production code resolving a real git.
func withFakeGitOnPath(t *testing.T) {
	t.Helper()

	src := buildFakeGit(t)
	dir := t.TempDir()

	name := "git"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	dst := filepath.Join(dir, name)

	data, err := os.ReadFile(src) //nolint:gosec // src is the path this package's own TestMain just built
	if err != nil {
		t.Fatalf("reading built fakegit binary: %v", err)
	}
	if err := os.WriteFile(dst, data, 0o755); err != nil { //nolint:gosec // dst is under t.TempDir(), executable on purpose
		t.Fatalf("writing fake git binary: %v", err)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestLocate_VersionTooOld(t *testing.T) {
	withFakeGitOnPath(t)
	t.Setenv("FAKEGIT_VERSION_OUTPUT", "git version 1.9.0\n")

	_, err := Locate(context.Background())
	if !errors.Is(err, ErrVersionTooOld) {
		t.Fatalf("Locate() error = %v, want ErrVersionTooOld", err)
	}
}

func TestLocate_VersionUnreadable(t *testing.T) {
	withFakeGitOnPath(t)
	t.Setenv("FAKEGIT_VERSION_OUTPUT", "not a version string at all\n")

	_, err := Locate(context.Background())
	if !errors.Is(err, ErrVersionUnreadable) {
		t.Fatalf("Locate() error = %v, want ErrVersionUnreadable", err)
	}
}

func TestLocate_AcceptsVersionAtOrAboveMinimum(t *testing.T) {
	withFakeGitOnPath(t)
	t.Setenv("FAKEGIT_VERSION_OUTPUT", "git version "+MinimumVersion.String()+"\n")

	bin, err := Locate(context.Background())
	if err != nil {
		t.Fatalf("Locate() returned an unexpected error: %v", err)
	}
	if bin.Version != MinimumVersion {
		t.Fatalf("bin.Version = %s, want %s", bin.Version, MinimumVersion)
	}
}

func TestLocateNamed_BinaryNotFound(t *testing.T) {
	_, err := LocateNamed(context.Background(), "atrio-gitops-test-binary-that-does-not-exist")
	if !errors.Is(err, ErrGitNotFound) {
		t.Fatalf("LocateNamed() error = %v, want ErrGitNotFound", err)
	}
}
