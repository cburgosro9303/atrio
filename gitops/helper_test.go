package gitops

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// fakeGitPath is the path to the compiled testdata/fakegit stand-in binary.
// It is built once in TestMain, before any test runs, and shared read-only by
// every test in this package: none of them need a fresh copy, and a compiled
// binary is used instead of a shell script so the same test works unmodified
// on every platform the CI matrix covers, including Windows, where
// "#!/bin/sh" is not something exec.LookPath / exec.Command can run.
var fakeGitPath string

func TestMain(m *testing.M) {
	os.Exit(runTestMain(m))
}

func runTestMain(m *testing.M) int {
	dir, err := os.MkdirTemp("", "atrio-gitops-fakegit-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "gitops tests: creating fakegit build dir:", err)
		return 1
	}
	defer func() {
		_ = os.RemoveAll(dir) //nolint:errcheck // best-effort cleanup of a test-only temp dir
	}()

	name := "fakegit"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	out := filepath.Join(dir, name)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// G204: "go" and every argument are literal strings fixed by this file,
	// building the fixed testdata/fakegit package — nothing here comes from
	// outside the test binary.
	cmd := exec.CommandContext(ctx, "go", "build", "-o", out, "./testdata/fakegit") //nolint:gosec // literal args, test-only build step
	if diagOut, buildErr := cmd.CombinedOutput(); buildErr != nil {
		fmt.Fprintf(os.Stderr, "gitops tests: building fakegit failed: %v\n%s\n", buildErr, diagOut)
		return 1
	}

	fakeGitPath = out
	return m.Run()
}

// buildFakeGit returns the path to the fakegit binary TestMain already built.
func buildFakeGit(t *testing.T) string {
	t.Helper()
	if fakeGitPath == "" {
		t.Fatal("fakegit was not built; TestMain should have built it before any test ran")
	}
	return fakeGitPath
}

// realGit locates the actual git binary installed on the machine running the
// tests. Status and identity behavior is exercised against real git rather
// than fakegit, since what matters there is that this package's parser
// matches what git really prints — see the task notes for why a temporary
// repository is preferred over depending on the user's own repository or
// global configuration.
//
// It fails the test rather than skipping when git cannot be located: git is a
// declared prerequisite of the platform (ADR-004) and present on every CI
// runner, so a broken Locate here must be visible as a failure, the same
// fail-closed stance internal/archtest takes for its own `go list` dependency
// — a silent skip would let every Status/Identity test report green while
// testing nothing.
func realGit(t *testing.T) *Binary {
	t.Helper()
	bin, err := Locate(context.Background())
	if err != nil {
		t.Fatalf("locating git for this test: %v", err)
	}
	return bin
}

// initRepo creates an empty git repository in a fresh temporary directory and
// returns its path. It does not configure user.name/user.email: callers that
// need to commit (most Status fixtures do) must set identity themselves, and
// callers testing Identity must not, which is the point of leaving it out
// here rather than defaulting it.
func initRepo(t *testing.T, bin *Binary) string {
	t.Helper()
	dir := t.TempDir()
	if _, _, err := bin.run(context.Background(), dir, "init", "--quiet"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	return dir
}

// setLocalIdentity configures user.name/user.email in dir's local (repository)
// git config, so commits made in tests do not depend on any ambient global
// configuration.
func setLocalIdentity(t *testing.T, bin *Binary, dir, name, email string) {
	t.Helper()
	if _, _, err := bin.run(context.Background(), dir, "config", "user.name", name); err != nil {
		t.Fatalf("git config user.name: %v", err)
	}
	if _, _, err := bin.run(context.Background(), dir, "config", "user.email", email); err != nil {
		t.Fatalf("git config user.email: %v", err)
	}
}

// isolateGitConfig points every config scope git reads outside the
// repository itself — system and global — at a directory that holds nothing,
// for the duration of the test, so reading (or writing) config inside a test
// repository cannot pick up whatever the machine running the tests happens to
// have configured.
//
// Two independent mechanisms are layered, because neither alone covers this
// package's whole supported version range (MinimumVersion 2.30.0):
//
//   - HOME (and USERPROFILE, git for Windows' fallback when HOME is unset) are
//     redirected to an empty temp directory, so git's default global-config
//     discovery — $HOME/.gitconfig, or $XDG_CONFIG_HOME/git/config, which is
//     also redirected here — finds nothing to read. This has worked since long
//     before 2.30 and is what actually isolates a git 2.30 or 2.31 binary.
//   - GIT_CONFIG_GLOBAL is additionally pointed at a nonexistent file: this is
//     the more direct override, but it was only introduced in git 2.32, so on
//     its own it would silently do nothing on the oldest binaries this package
//     still accepts — exactly the gap that made the HOME override necessary
//     rather than optional.
//
// GIT_CONFIG_NOSYSTEM covers the system scope and has existed since long
// before MinimumVersion, on every platform.
func isolateGitConfig(t *testing.T) {
	t.Helper()
	empty := t.TempDir()
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(empty, "nonexistent-global-gitconfig"))
	t.Setenv("HOME", empty)
	t.Setenv("USERPROFILE", empty)
	t.Setenv("XDG_CONFIG_HOME", empty)
}
