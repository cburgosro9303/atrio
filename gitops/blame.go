package gitops

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Attribution names who last changed a file, and in which commit.
type Attribution struct {
	// Commit is the full 40-hex object name (SHA) of the commit that last
	// touched the file.
	Commit string

	// Name and Email are the author name and email git recorded for Commit,
	// exactly as `%an`/`%ae` report them.
	Name  string
	Email string
}

// ErrNoAttribution means git has no commit that touches the requested path:
// the file is untracked (never committed, including "just created and not
// yet staged") or does not exist at all. Neither is a system failure —
// store/localdb.sql gives document_issue's attribution columns an empty
// default precisely "so that detecting an invalid document never depends on
// git being answerable at that moment" — so callers must be able to tell
// this case apart from a real error. Use errors.Is(err, ErrNoAttribution).
var ErrNoAttribution = errors.New("gitops: no commit touches this file")

// LastEditor identifies who last changed path, and in which commit.
//
// The T-022 backlog line for this task asks for "rastreo de autor (git
// blame del cambio)" — author tracking via "the git blame of the change".
// What actually has to be produced, though, are the three document_issue
// columns this feeds (attributed_name, attributed_email, attributed_commit):
// one attribution per file, not one per line. `git blame` answers "who wrote
// this specific line", the wrong shape here without inventing a policy this
// task has no basis for: "the most recent commit among the blamed lines"
// would need a tie-break rule (author-time is user-configurable and can move
// backward; committer-time has the same problem), plus a further tie-break
// by hash — neither of which the spec gives, and this task is named
// *deterministic*. A document whose front matter fails to parse may also
// have no identifiable line range to blame at all. So the rule implemented
// here needs no invented tie-break: the most recent commit that touched the
// file, i.e. `git log -1 -- path`.
//
// The exact invocation is:
//
//	git --literal-pathspecs log -1 --format=%H%x00%an%x00%ae -- <path>
//
// Three details are load-bearing, each defending against a distinct gap:
//
//   - --literal-pathspecs (a global option, placed before the subcommand)
//     disables git's pathspec magic entirely. The "--" separator below stops
//     a value starting with "-" from being read as an option, but it does
//     NOT turn off pathspec magic on its own: a path starting with ":" (for
//     instance ":(glob)docs/*", or plain ":foo") is still read as a magic
//     pathspec even after "--". This flag is the other half "--" alone does
//     not cover — verified empirically: without it, `git log -1 --
//     :colon.txt` silently exits 0 with no output for a file that is
//     actually tracked and has a commit, exactly the class of bug this
//     function exists to avoid.
//   - The "--" separator before path closes the case of a value starting
//     with "-", which git would otherwise try to parse as an option.
//   - %x00 as the field separator, instead of %n or plain whitespace: the
//     same reasoning that led to -z in Status (T-030) — each field arrives
//     verbatim, with no path-quoting rules to reimplement as a second,
//     harder-to-audit parser.
//
// This is exactly the point T-030 left as pending: "the point where the
// first external value enters args ...string". LastEditor is that point for
// this package.
//
// path is relative to dir, in forward-slash form (the same convention
// StatusEntry.Path uses); LastEditor does not transform it. --follow is
// deliberately not used: following renames would change the question this
// answers from "who last touched this file" to "who last touched its
// ancestor", and it is the former callers need.
//
// Semantics:
//
//   - Empty output (git exits 0 but prints nothing) means no commit touches
//     path — untracked, or simply nonexistent. LastEditor reports this as
//     ErrNoAttribution, not a system error.
//   - Output with other than exactly three NUL-separated fields, or whose
//     first field is not exactly 40 lowercase hex characters, is an explicit
//     error: the parser is total, like parsePorcelainV1Z in status.go — any
//     deviation is a named error, never a blank or partially-trusted field
//     let through silently. (A SHA-256 repository, `git init
//     --object-format=sha256`, would print a 64-hex %H and be rejected here
//     as malformed; this package has no SHA-256 test coverage or support
//     elsewhere, so that is a known limitation, not an oversight.)
//
// Known gap, not handled here: a repository with no commits at all (no HEAD
// yet) makes `git log` itself fail ("does not have any commits yet", exit
// 128), which surfaces as a wrapped error, not ErrNoAttribution — even
// though "no attribution available" is arguably the right characterization.
// Deciding whether to special-case that is left to whichever caller first
// needs it, since the task text only specifies empty *output* (a git log
// that runs and finds nothing), not a git log that cannot run at all.
func (b *Binary) LastEditor(ctx context.Context, dir, path string) (Attribution, error) {
	stdout, _, err := b.run(ctx, dir,
		"--literal-pathspecs",
		"log", "-1", "--format=%H%x00%an%x00%ae",
		"--", path,
	)
	if err != nil {
		return Attribution{}, fmt.Errorf("gitops: git log for %q: %w", path, err)
	}
	return parseLastEditorOutput(path, stdout)
}

// parseLastEditorOutput parses the output of the `git log -1
// --format=%H%x00%an%x00%ae` invocation LastEditor runs. It is total on its
// input: anything other than exactly three NUL-separated fields, or a first
// field that is not exactly 40 lowercase hex characters, is a named error
// rather than a partially-trusted Attribution.
func parseLastEditorOutput(path, raw string) (Attribution, error) {
	if raw == "" {
		return Attribution{}, fmt.Errorf("%w: %q", ErrNoAttribution, path)
	}

	// Fields are split first, on \x00 — the actual field separator — before
	// any trimming happens, so a name or email is never touched. Only the
	// last field carries git's own trailing record terminator (a single "\n"
	// after the whole --format record, added once per commit, never part of
	// %an/%ae's content: git's commit object format keeps the author/
	// committer ident on one line, so a raw newline cannot appear inside
	// either field to begin with — confirmed empirically: a user.name
	// configured with an embedded newline is stripped down to one line by
	// git itself before it ever reaches this parser). That terminator is
	// trimmed from the last field alone, not from the whole record, so
	// nothing here depends on assuming where in the record it is.
	fields := strings.Split(raw, "\x00")
	if len(fields) != 3 {
		return Attribution{}, fmt.Errorf(
			"gitops: malformed git log output for %q: expected 3 NUL-separated fields, got %d: %q",
			path, len(fields), raw)
	}

	commit, name := fields[0], fields[1]
	email := strings.TrimRight(fields[2], "\r\n")
	if !isCommitHash(commit) {
		return Attribution{}, fmt.Errorf(
			"gitops: malformed git log output for %q: %q is not a 40-character lowercase hex commit hash",
			path, commit)
	}

	return Attribution{Commit: commit, Name: name, Email: email}, nil
}

// isCommitHash reports whether s is exactly 40 lowercase hexadecimal
// characters — the form %H always takes for the SHA-1 object names every
// git version this package supports (MinimumVersion 2.30.0) uses.
func isCommitHash(s string) bool {
	if len(s) != 40 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
