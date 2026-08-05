package gitops

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// writeFile is a small test convenience: create/overwrite a file with content.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func gitRun(t *testing.T, bin *Binary, dir string, args ...string) {
	t.Helper()
	if _, stderr, err := bin.run(context.Background(), dir, args...); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, stderr)
	}
}

// TestBinary_Status_MultipleStatesAtOnce exercises Status against a real,
// temporary repository holding several different states simultaneously: a
// staged addition, an unstaged modification, a staged modification, an
// unstaged deletion, a staged rename and an untracked file. This is the
// combination the task calls out explicitly, and it is what proves the
// parser handles a realistic `git status` output, not just one line at a
// time in isolation.
func TestBinary_Status_MultipleStatesAtOnce(t *testing.T) {
	bin := realGit(t)
	isolateGitConfig(t) // a system-wide autocrlf/showUntrackedFiles setting must not change this result
	dir := initRepo(t, bin)
	setLocalIdentity(t, bin, dir, "Status Test", "status@test.local")

	// Committed baseline.
	writeFile(t, dir, "unstaged-modify.txt", "original\n")
	writeFile(t, dir, "staged-modify.txt", "original\n")
	writeFile(t, dir, "to-delete.txt", "bye\n")
	writeFile(t, dir, "to-rename.txt", "rename me\n")
	gitRun(t, bin, dir, "add", ".")
	gitRun(t, bin, dir, "commit", "--quiet", "-m", "baseline")

	// Unstaged modification.
	writeFile(t, dir, "unstaged-modify.txt", "changed\n")

	// Staged modification.
	writeFile(t, dir, "staged-modify.txt", "changed\n")
	gitRun(t, bin, dir, "add", "staged-modify.txt")

	// Unstaged deletion.
	if err := os.Remove(filepath.Join(dir, "to-delete.txt")); err != nil {
		t.Fatalf("removing to-delete.txt: %v", err)
	}

	// Staged rename.
	gitRun(t, bin, dir, "mv", "to-rename.txt", "renamed.txt")

	// Staged addition (new file, never committed).
	writeFile(t, dir, "staged-new.txt", "new\n")
	gitRun(t, bin, dir, "add", "staged-new.txt")

	// Untracked file.
	writeFile(t, dir, "untracked.txt", "untracked\n")

	entries, err := bin.Status(context.Background(), dir)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	byPath := make(map[string]StatusEntry, len(entries))
	for _, e := range entries {
		byPath[e.Path] = e
	}

	want := map[string]StatusEntry{
		"unstaged-modify.txt": {IndexStatus: ' ', WorktreeStatus: 'M', Path: "unstaged-modify.txt"},
		"staged-modify.txt":   {IndexStatus: 'M', WorktreeStatus: ' ', Path: "staged-modify.txt"},
		"to-delete.txt":       {IndexStatus: ' ', WorktreeStatus: 'D', Path: "to-delete.txt"},
		"renamed.txt": {
			IndexStatus: 'R', WorktreeStatus: ' ', Path: "renamed.txt", OrigPath: "to-rename.txt",
		},
		"staged-new.txt": {IndexStatus: 'A', WorktreeStatus: ' ', Path: "staged-new.txt"},
		"untracked.txt":  {IndexStatus: '?', WorktreeStatus: '?', Path: "untracked.txt"},
	}

	if len(byPath) != len(want) {
		t.Fatalf("Status returned %d entries, want %d: %+v", len(byPath), len(want), entries)
	}
	for path, wantEntry := range want {
		got, ok := byPath[path]
		if !ok {
			t.Fatalf("Status: missing entry for %q, got %+v", path, entries)
		}
		if got != wantEntry {
			t.Fatalf("Status entry for %q = %+v, want %+v", path, got, wantEntry)
		}
	}

	if !byPath["untracked.txt"].IsUntracked() {
		t.Fatal("untracked.txt: IsUntracked() = false, want true")
	}
}

func TestBinary_Status_CleanRepo(t *testing.T) {
	bin := realGit(t)
	isolateGitConfig(t)
	dir := initRepo(t, bin)
	setLocalIdentity(t, bin, dir, "Status Test", "status@test.local")

	writeFile(t, dir, "a.txt", "a\n")
	gitRun(t, bin, dir, "add", ".")
	gitRun(t, bin, dir, "commit", "--quiet", "-m", "init")

	entries, err := bin.Status(context.Background(), dir)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("Status on a clean repo = %+v, want no entries", entries)
	}
}

// TestParsePorcelainV1Z_WellFormed exercises the pure parser directly against
// crafted byte sequences, independent of a real git process, so the exact
// framing (three-byte prefix, NUL-terminated tokens, the extra field for
// renames/copies) is pinned down precisely.
func TestParsePorcelainV1Z_WellFormed(t *testing.T) {
	raw := "A  added.txt\x00 M modified.txt\x00R  new-name.txt\x00old-name.txt\x00" +
		"?? untracked.txt\x00 T typechanged.txt\x00"

	entries, err := parsePorcelainV1Z(raw)
	if err != nil {
		t.Fatalf("parsePorcelainV1Z: %v", err)
	}

	want := []StatusEntry{
		{IndexStatus: 'A', WorktreeStatus: ' ', Path: "added.txt"},
		{IndexStatus: ' ', WorktreeStatus: 'M', Path: "modified.txt"},
		{IndexStatus: 'R', WorktreeStatus: ' ', Path: "new-name.txt", OrigPath: "old-name.txt"},
		{IndexStatus: '?', WorktreeStatus: '?', Path: "untracked.txt"},
		// 'T': a regular file replaced by a symlink (or vice versa) — a normal
		// repository state git status reports independently of 'M'.
		{IndexStatus: ' ', WorktreeStatus: 'T', Path: "typechanged.txt"},
	}
	if len(entries) != len(want) {
		t.Fatalf("parsePorcelainV1Z returned %d entries, want %d: %+v", len(entries), len(want), entries)
	}
	for i, e := range entries {
		if e != want[i] {
			t.Fatalf("entry %d = %+v, want %+v", i, e, want[i])
		}
	}
}

func TestParsePorcelainV1Z_EmptyIsNoEntries(t *testing.T) {
	entries, err := parsePorcelainV1Z("")
	if err != nil {
		t.Fatalf("parsePorcelainV1Z(\"\") returned error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("parsePorcelainV1Z(\"\") = %+v, want no entries", entries)
	}
}

// TestParsePorcelainV1Z_Malformed is the "total parser" requirement: a line
// this package does not recognize must be a reported error, never a silently
// dropped or half-parsed entry.
func TestParsePorcelainV1Z_Malformed(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"token too short", "M\x00"},
		{"missing separator space", "MXfile.txt\x00"},
		{"unknown index status", "X  file.txt\x00"},
		{"unknown worktree status", " X file.txt\x00"},
		{"mismatched untracked marker", "?M file.txt\x00"},
		{"mismatched ignored marker", "!M file.txt\x00"},
		{"rename missing origin path", "R  new-name.txt\x00"},
		{"copy missing origin path", "C  copy.txt\x00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parsePorcelainV1Z(tt.raw)
			if err == nil {
				t.Fatalf("parsePorcelainV1Z(%q) returned no error, want a malformed-line error", tt.raw)
			}
		})
	}
}
