package gitops

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestRunRaw_NoShellInterpolation is the test that matters most in this
// package: it proves that arguments reach the git process exactly as given,
// with no shell ever involved to reinterpret them.
//
// It sends arguments built to be dangerous if any code path concatenated them
// into a shell command line — command separators, command substitution,
// quoting, redirection and embedded spaces — and asserts the fake binary
// received them byte-for-byte. exec.CommandContext never spawns a shell, so
// this also stands as a regression test: if a future change routed git
// invocations through `sh -c` or similar, either this test would fail (the
// dumped argv would not match, because the shell would have consumed some of
// these characters) or the metacharacters would take effect on the test
// runner, which would be a far worse signal.
func TestRunRaw_NoShellInterpolation(t *testing.T) {
	fakeGit := buildFakeGit(t)
	dumpPath := filepath.Join(t.TempDir(), "argv.json")
	t.Setenv("FAKEGIT_ARGV_DUMP", dumpPath)

	dangerous := []string{
		"status; rm -rf /",
		"$(whoami)",
		"`id`",
		"--flag=\"quoted value\"",
		"has spaces and\ttabs",
		"a && b || c",
		"path/with/../traversal",
		"",
	}

	_, _, err := runRaw(context.Background(), fakeGit, "", dangerous...)
	if err != nil {
		t.Fatalf("runRaw returned an unexpected error: %v", err)
	}

	raw, err := os.ReadFile(dumpPath) //nolint:gosec // dumpPath is built from t.TempDir() above
	if err != nil {
		t.Fatalf("reading argv dump: %v", err)
	}

	var got []string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decoding argv dump: %v", err)
	}

	if !reflect.DeepEqual(got, dangerous) {
		t.Fatalf("argv reached the process altered:\n  sent: %#v\n  got:  %#v", dangerous, got)
	}
}

func TestRunRaw_ExitCodeSurfacesAsRunError(t *testing.T) {
	fakeGit := buildFakeGit(t)
	t.Setenv("FAKEGIT_EXIT_CODE", "7")

	_, _, err := runRaw(context.Background(), fakeGit, "", "whatever")
	if err == nil {
		t.Fatal("expected an error for a non-zero exit code, got nil")
	}

	var rErr *runError
	if !errors.As(err, &rErr) {
		t.Fatalf("expected a *runError, got %T: %v", err, err)
	}
	if got := rErr.exitCode(); got != 7 {
		t.Fatalf("exitCode() = %d, want 7", got)
	}
}

func TestRunRaw_MissingBinary(t *testing.T) {
	_, _, err := runRaw(context.Background(), filepath.Join(t.TempDir(), "does-not-exist"), "", "--version")
	if err == nil {
		t.Fatal("expected an error for a nonexistent binary, got nil")
	}
}
