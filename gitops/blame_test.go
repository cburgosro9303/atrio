package gitops

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// gitOutput runs a git subcommand in dir and returns its trimmed stdout,
// failing the test on error. It exists alongside gitRun (status_test.go)
// because gitRun discards stdout, and these tests need it (e.g. `git
// rev-parse HEAD` to compare against Attribution.Commit).
func gitOutput(t *testing.T, bin *Binary, dir string, args ...string) string {
	t.Helper()
	stdout, stderr, err := bin.run(context.Background(), dir, args...)
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, stderr)
	}
	return strings.TrimSpace(stdout)
}

// addAndCommit stages name with --literal-pathspecs (so a leading ':' is
// never read as pathspec magic, matching what LastEditor itself does to
// read it back) and commits it under the given identity.
func addAndCommit(t *testing.T, bin *Binary, dir, name, content, authorName, authorEmail, message string) {
	t.Helper()
	writeFile(t, dir, name, content)
	gitRun(t, bin, dir, "--literal-pathspecs", "add", "--", name)
	setLocalIdentity(t, bin, dir, authorName, authorEmail)
	gitRun(t, bin, dir, "--literal-pathspecs", "commit", "--quiet", "-m", message)
}

// TestBinary_LastEditor_TwoCommitsDifferentIdentities is the primary
// determinism check: when the same file is committed twice under different
// identities, LastEditor must report the second (most recent) one, with the
// full commit hash matching `git rev-parse HEAD` exactly.
func TestBinary_LastEditor_TwoCommitsDifferentIdentities(t *testing.T) {
	bin := realGit(t)
	isolateGitConfig(t)
	dir := initRepo(t, bin)

	addAndCommit(t, bin, dir, "doc.md", "first version\n", "Ada Lovelace", "ada@example.com", "first")
	addAndCommit(t, bin, dir, "doc.md", "second version\n", "Alan Turing", "alan@example.com", "second")

	wantCommit := gitOutput(t, bin, dir, "rev-parse", "HEAD")

	got, err := bin.LastEditor(context.Background(), dir, "doc.md")
	if err != nil {
		t.Fatalf("LastEditor: %v", err)
	}
	want := Attribution{Commit: wantCommit, Name: "Alan Turing", Email: "alan@example.com"}
	if got != want {
		t.Fatalf("LastEditor = %+v, want %+v", got, want)
	}
}

// TestBinary_LastEditor_UnrelatedLaterCommitDoesNotChangeAttribution proves
// LastEditor answers "who last touched this file", not "who made the last
// commit in the repository": a later commit that leaves the file alone must
// not shadow the earlier commit that actually touched it.
func TestBinary_LastEditor_UnrelatedLaterCommitDoesNotChangeAttribution(t *testing.T) {
	bin := realGit(t)
	isolateGitConfig(t)
	dir := initRepo(t, bin)

	addAndCommit(t, bin, dir, "target.md", "target content\n", "Ada Lovelace", "ada@example.com", "add target")
	wantCommit := gitOutput(t, bin, dir, "rev-parse", "HEAD")

	// A later commit that touches a different file entirely.
	addAndCommit(t, bin, dir, "other.md", "other content\n", "Alan Turing", "alan@example.com", "add other")

	got, err := bin.LastEditor(context.Background(), dir, "target.md")
	if err != nil {
		t.Fatalf("LastEditor: %v", err)
	}
	want := Attribution{Commit: wantCommit, Name: "Ada Lovelace", Email: "ada@example.com"}
	if got != want {
		t.Fatalf("LastEditor = %+v, want %+v (must stay attributed to the commit that touched target.md)", got, want)
	}
}

// TestBinary_LastEditor_UserNameWithEmbeddedNewlineIsCollapsed pins the
// claim parseLastEditorOutput's own comment makes: a raw newline cannot
// appear inside %an because git's commit object format keeps the
// author/committer ident on one line, collapsing whatever was configured
// before it ever reaches this parser. That claim was living on an
// unexercised assumption; this test is what turns it into a checked fact.
//
// Confirmed by hand before writing this test, and worth recording exactly
// what was confirmed: `git config user.name` itself stores the value
// verbatim, embedded newline and all — `git config user.name` reads it back
// as two lines. It is specifically `git log --format=%an` (what
// LastEditor's own invocation uses) that collapses it, and it does so by
// deleting the newline outright rather than substituting a space:
// "line1\nline2" becomes "line1line2" in the log output, which is the exact
// value this test asserts. Had either half of that turned out false — the
// value rejected outright, or the newline surviving into --format output —
// the comment this test backs would have been wrong, not this test.
func TestBinary_LastEditor_UserNameWithEmbeddedNewlineIsCollapsed(t *testing.T) {
	bin := realGit(t)
	isolateGitConfig(t)
	dir := initRepo(t, bin)

	addAndCommit(t, bin, dir, "doc.md", "content\n", "line1\nline2", "ada@example.com", "add doc")

	got, err := bin.LastEditor(context.Background(), dir, "doc.md")
	if err != nil {
		t.Fatalf("LastEditor: %v", err)
	}
	if strings.Contains(got.Name, "\n") {
		t.Fatalf("Name = %q, still carries the embedded newline — parseLastEditorOutput's comment about git collapsing it is wrong, not this test", got.Name)
	}
	wantName := "line1line2"
	if got.Name != wantName {
		t.Fatalf("Name = %q, want %q (git deletes the newline outright rather than inserting a separator)", got.Name, wantName)
	}
}

func TestBinary_LastEditor_UntrackedFile(t *testing.T) {
	bin := realGit(t)
	isolateGitConfig(t)
	dir := initRepo(t, bin)
	// The repo needs at least one commit so `git log` has a HEAD to search
	// from; without it git fails with "does not have any commits yet" (a
	// real error, exit 128), which is a different and less interesting
	// reason for producing no output than "this file has none".
	addAndCommit(t, bin, dir, "committed.md", "content\n", "Ada Lovelace", "ada@example.com", "init")

	writeFile(t, dir, "untracked.md", "never committed\n")

	_, err := bin.LastEditor(context.Background(), dir, "untracked.md")
	if !errors.Is(err, ErrNoAttribution) {
		t.Fatalf("LastEditor error = %v, want ErrNoAttribution", err)
	}
}

func TestBinary_LastEditor_NonexistentFile(t *testing.T) {
	bin := realGit(t)
	isolateGitConfig(t)
	dir := initRepo(t, bin)
	setLocalIdentity(t, bin, dir, "Ada Lovelace", "ada@example.com")
	// The repo needs at least one commit so `git log` has history to search;
	// without it git simply has no HEAD, which is a different (and less
	// interesting) reason for empty output.
	addAndCommit(t, bin, dir, "exists.md", "content\n", "Ada Lovelace", "ada@example.com", "init")

	_, err := bin.LastEditor(context.Background(), dir, "does-not-exist.md")
	if !errors.Is(err, ErrNoAttribution) {
		t.Fatalf("LastEditor error = %v, want ErrNoAttribution", err)
	}
}

// TestBinary_LastEditor_ArgumentDiscipline exercises the three path shapes
// that motivate this function's exact command line: a value that could be
// misread as a git option, and a value that could be misread as pathspec
// magic even after "--". Each subtest creates and commits a real file under
// that name and checks LastEditor attributes *that* file's own commit, not
// some other one.
func TestBinary_LastEditor_ArgumentDiscipline(t *testing.T) {
	tests := []struct {
		name          string
		fileName      string
		skipOnWindows string // non-empty: skip reason on windows
	}{
		{name: "leading dash", fileName: "-dashed.md"},
		{
			name:     "leading colon",
			fileName: ":colon.md",
			// The case "--" alone does not cover: without --literal-pathspecs,
			// ":colon.md" is read as a (here nonsensical) magic pathspec.
			skipOnWindows: "Windows reserves ':' in filenames, so this file cannot be created",
		},
		{name: "spaces and non-ASCII", fileName: "héllo world.md"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skipOnWindows != "" && runtime.GOOS == "windows" {
				t.Skip(tt.skipOnWindows)
			}

			bin := realGit(t)
			isolateGitConfig(t)
			dir := initRepo(t, bin)
			addAndCommit(t, bin, dir, tt.fileName, "content\n", "Grace Hopper", "grace@example.com", "add "+tt.name)
			wantCommit := gitOutput(t, bin, dir, "rev-parse", "HEAD")

			got, err := bin.LastEditor(context.Background(), dir, tt.fileName)
			if err != nil {
				t.Fatalf("LastEditor(%q): %v", tt.fileName, err)
			}
			want := Attribution{Commit: wantCommit, Name: "Grace Hopper", Email: "grace@example.com"}
			if got != want {
				t.Fatalf("LastEditor(%q) = %+v, want %+v", tt.fileName, got, want)
			}
		})
	}
}

// TestBinary_LastEditor_ArgvUsesLiteralPathspecs asserts the exact shape of
// the command LastEditor builds, using the fakegit stand-in to dump argv —
// the same technique TestRunRaw_NoShellInterpolation uses. This is what
// proves --literal-pathspecs, "--" and the NUL-separated --format string are
// actually in the invocation, not just documented as intent.
func TestBinary_LastEditor_ArgvUsesLiteralPathspecs(t *testing.T) {
	fakeGit := buildFakeGit(t)
	dumpPath := filepath.Join(t.TempDir(), "argv.json")
	t.Setenv("FAKEGIT_ARGV_DUMP", dumpPath)

	bin := &Binary{Path: fakeGit}
	// fakegit prints nothing by default, so this resolves to ErrNoAttribution;
	// what this test cares about is the argv it was given, checked below, but
	// the error is still asserted rather than discarded.
	_, gotErr := bin.LastEditor(context.Background(), t.TempDir(), "some/path.md")
	if !errors.Is(gotErr, ErrNoAttribution) {
		t.Fatalf("LastEditor error = %v, want ErrNoAttribution", gotErr)
	}

	raw, err := os.ReadFile(dumpPath) //nolint:gosec // dumpPath is built from t.TempDir() above
	if err != nil {
		t.Fatalf("reading argv dump: %v", err)
	}
	var got []string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decoding argv dump: %v", err)
	}

	want := []string{
		"--literal-pathspecs",
		"log", "-1", "--format=%H%x00%an%x00%ae",
		"--", "some/path.md",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %#v, want %#v", got, want)
	}
}

// TestParseLastEditorOutput_Malformed is the "total parser" requirement for
// LastEditor's output, exercised directly against crafted byte sequences via
// fakegit's FAKEGIT_STDOUT — mirroring how TestParsePorcelainV1Z_Malformed
// covers status.go.
func TestParseLastEditorOutput_Malformed(t *testing.T) {
	validHash := strings.Repeat("a", 40)
	tests := []struct {
		name string
		raw  string
	}{
		{"only one field", "onefield\n"},
		{"only two fields", validHash + "\x00Name Only\n"},
		{"four fields", validHash + "\x00Name\x00email@example.com\x00extra\n"},
		{"hash too short", "abc\x00Name\x00email@example.com\n"},
		{"hash not hex", strings.Repeat("g", 40) + "\x00Name\x00email@example.com\n"},
		{"hash uppercase", strings.Repeat("A", 40) + "\x00Name\x00email@example.com\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseLastEditorOutput("some/path.md", tt.raw)
			if err == nil {
				t.Fatalf("parseLastEditorOutput(%q) returned no error, want a malformed-output error", tt.raw)
			}
			if errors.Is(err, ErrNoAttribution) {
				t.Fatalf("parseLastEditorOutput(%q) = %v, want a distinct error from ErrNoAttribution", tt.raw, err)
			}
		})
	}
}

// TestBinary_LastEditor_MalformedOutputThroughFakeGit exercises the same
// malformed-output rejection end to end, through Binary.LastEditor and a
// real (fake) subprocess, rather than calling the pure parser directly.
func TestBinary_LastEditor_MalformedOutputThroughFakeGit(t *testing.T) {
	fakeGit := buildFakeGit(t)
	t.Setenv("FAKEGIT_STDOUT", "not-a-valid-record\n")

	bin := &Binary{Path: fakeGit}
	_, err := bin.LastEditor(context.Background(), t.TempDir(), "some/path.md")
	if err == nil {
		t.Fatal("LastEditor returned no error for malformed output, want a malformed-output error")
	}
	if errors.Is(err, ErrNoAttribution) {
		t.Fatalf("LastEditor error = %v, want a distinct error from ErrNoAttribution", err)
	}
}

func TestParseLastEditorOutput_EmptyIsNoAttribution(t *testing.T) {
	_, err := parseLastEditorOutput("some/path.md", "")
	if !errors.Is(err, ErrNoAttribution) {
		t.Fatalf("parseLastEditorOutput(\"\") error = %v, want ErrNoAttribution", err)
	}
}

func TestParseLastEditorOutput_WellFormed(t *testing.T) {
	hash := strings.Repeat("f", 40)
	raw := hash + "\x00Ada Lovelace\x00ada@example.com\n"

	got, err := parseLastEditorOutput("some/path.md", raw)
	if err != nil {
		t.Fatalf("parseLastEditorOutput: %v", err)
	}
	want := Attribution{Commit: hash, Name: "Ada Lovelace", Email: "ada@example.com"}
	if got != want {
		t.Fatalf("parseLastEditorOutput = %+v, want %+v", got, want)
	}
}
