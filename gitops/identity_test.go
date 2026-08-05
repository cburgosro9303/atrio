package gitops

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestBinary_Identity_Complete(t *testing.T) {
	bin := realGit(t)
	isolateGitConfig(t)
	dir := initRepo(t, bin)
	setLocalIdentity(t, bin, dir, "Ada Lovelace", "ada@example.com")

	got, err := bin.Identity(context.Background(), dir)
	if err != nil {
		t.Fatalf("Identity: %v", err)
	}
	want := Identity{Name: "Ada Lovelace", Email: "ada@example.com"}
	if got != want {
		t.Fatalf("Identity = %+v, want %+v", got, want)
	}
}

func TestBinary_Identity_NameOnly(t *testing.T) {
	bin := realGit(t)
	isolateGitConfig(t)
	dir := initRepo(t, bin)
	gitRun(t, bin, dir, "config", "user.name", "Ada Lovelace")

	_, err := bin.Identity(context.Background(), dir)
	assertIncompleteIdentity(t, err, "user.email")
}

func TestBinary_Identity_EmailOnly(t *testing.T) {
	bin := realGit(t)
	isolateGitConfig(t)
	dir := initRepo(t, bin)
	gitRun(t, bin, dir, "config", "user.email", "ada@example.com")

	_, err := bin.Identity(context.Background(), dir)
	assertIncompleteIdentity(t, err, "user.name")
}

func TestBinary_Identity_Neither(t *testing.T) {
	bin := realGit(t)
	isolateGitConfig(t)
	dir := initRepo(t, bin)

	_, err := bin.Identity(context.Background(), dir)
	assertIncompleteIdentity(t, err, "user.name", "user.email")
}

// TestBinary_Identity_EmptyValueTreatedAsMissing covers a value explicitly set
// to the empty string, which is not a usable identity component even though
// `git config --get` reports it as present (exit 0, empty stdout) rather than
// unset.
func TestBinary_Identity_EmptyValueTreatedAsMissing(t *testing.T) {
	bin := realGit(t)
	isolateGitConfig(t)
	dir := initRepo(t, bin)
	gitRun(t, bin, dir, "config", "user.name", "")
	gitRun(t, bin, dir, "config", "user.email", "ada@example.com")

	_, err := bin.Identity(context.Background(), dir)
	assertIncompleteIdentity(t, err, "user.name")
}

func assertIncompleteIdentity(t *testing.T, err error, mustMention ...string) {
	t.Helper()
	if !errors.Is(err, ErrIdentityIncomplete) {
		t.Fatalf("Identity error = %v, want ErrIdentityIncomplete", err)
	}
	for _, key := range mustMention {
		if !strings.Contains(err.Error(), key) {
			t.Fatalf("Identity error %q does not mention %q", err.Error(), key)
		}
	}
}
