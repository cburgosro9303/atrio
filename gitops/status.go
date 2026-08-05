package gitops

import (
	"context"
	"fmt"
	"strings"
)

// StatusEntry is one parsed line of `git status --porcelain=v1 -z` output: a
// two-character index/worktree status pair plus the path(s) it applies to.
type StatusEntry struct {
	// IndexStatus is the first status character (X in git's documentation):
	// the state of the index/staging area relative to HEAD.
	IndexStatus byte

	// WorktreeStatus is the second status character (Y): the state of the
	// working tree relative to the index.
	WorktreeStatus byte

	// Path is the file path this entry describes. For a rename or copy, this
	// is the new path.
	Path string

	// OrigPath is set only when IndexStatus or WorktreeStatus is 'R' (renamed)
	// or 'C' (copied): the path the entry was renamed or copied from.
	OrigPath string
}

// IsUntracked reports whether the entry is an untracked file ("??").
func (e StatusEntry) IsUntracked() bool {
	return e.IndexStatus == '?' && e.WorktreeStatus == '?'
}

// knownStatusLetters are the status characters git status --porcelain=v1
// documents for the index and worktree columns, excluding the paired markers
// '?' (untracked) and '!' (ignored), which validateStatusPair handles
// separately because they may only appear as a matching pair. 'T' (typechange,
// e.g. a regular file replaced by a symlink) is included: it is a normal
// repository state, not an edge case, and rejecting it would turn a routine
// `Status` call into a hard failure.
const knownStatusLetters = " MADRCUT"

// Status runs `git status --porcelain=v1 -z` in dir and parses its output.
//
// -z is requested (rather than the default, human-oriented quoting of
// "unusual" paths) so that every path is delivered verbatim and
// NUL-terminated: parsing does not have to re-implement git's C-style quoting
// rules for paths with spaces, quotes or non-ASCII bytes, which would be a
// second, harder-to-audit place where "total parsing" could quietly fail.
//
// --untracked-files=normal pins git's own default explicitly. Without it, a
// user with status.showUntrackedFiles=no in their git config would silently
// get untracked files omitted from the result — a caller using Status to
// decide whether a worktree is clean would then see "clean" over a worktree
// that is not, exactly the kind of silent gap this parser exists to avoid.
func (b *Binary) Status(ctx context.Context, dir string) ([]StatusEntry, error) {
	stdout, _, err := b.run(ctx, dir, "status", "--porcelain=v1", "--untracked-files=normal", "-z")
	if err != nil {
		return nil, fmt.Errorf("gitops: git status: %w", err)
	}
	return parsePorcelainV1Z(stdout)
}

// parsePorcelainV1Z parses the NUL-separated output of
// `git status --porcelain=v1 -z`.
//
// It is total on its input: a token that is too short, whose status
// characters are not ones git documents, or whose rename/copy marker is
// missing its second (origin path) field is reported as an error rather than
// skipped. Silently dropping an unrecognized line would mean the platform
// could observe a repository as cleaner than it actually is.
func parsePorcelainV1Z(raw string) ([]StatusEntry, error) {
	if raw == "" {
		return nil, nil
	}

	tokens := strings.Split(raw, "\x00")
	// -z terminates every record with a NUL, including the last one, which
	// leaves one trailing empty token after Split.
	if len(tokens) > 0 && tokens[len(tokens)-1] == "" {
		tokens = tokens[:len(tokens)-1]
	}

	entries := make([]StatusEntry, 0, len(tokens))
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if len(tok) < 3 || tok[2] != ' ' {
			return nil, fmt.Errorf("gitops: malformed git status line %q: expected \"XY \" prefix", tok)
		}

		x, y := tok[0], tok[1]
		if err := validateStatusPair(x, y); err != nil {
			return nil, fmt.Errorf("gitops: malformed git status line %q: %w", tok, err)
		}

		entry := StatusEntry{IndexStatus: x, WorktreeStatus: y, Path: tok[3:]}
		if x == 'R' || x == 'C' || y == 'R' || y == 'C' {
			i++
			if i >= len(tokens) {
				return nil, fmt.Errorf(
					"gitops: malformed git status line %q: rename/copy is missing its origin path", tok)
			}
			entry.OrigPath = tokens[i]
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// validateStatusPair rejects any XY pair parsePorcelainV1Z does not
// recognize, rather than letting it through with unclear meaning.
func validateStatusPair(x, y byte) error {
	switch {
	case x == '?' || y == '?':
		if x != '?' || y != '?' {
			return fmt.Errorf("'?' (untracked) must appear in both columns, got %q%q", x, y)
		}
		return nil
	case x == '!' || y == '!':
		if x != '!' || y != '!' {
			return fmt.Errorf("'!' (ignored) must appear in both columns, got %q%q", x, y)
		}
		return nil
	}

	if !strings.ContainsRune(knownStatusLetters, rune(x)) {
		return fmt.Errorf("unknown index status %q", x)
	}
	if !strings.ContainsRune(knownStatusLetters, rune(y)) {
		return fmt.Errorf("unknown worktree status %q", y)
	}
	return nil
}
