package gitops

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
)

// DefaultBinaryName is the executable name Locate looks up on PATH.
const DefaultBinaryName = "git"

// ErrGitNotFound means no git executable could be found on PATH. The
// architecture doc (ADR-004) declares the system git binary a prerequisite of
// the platform, on the same footing as a provider CLI: Atrio does not vendor
// or embed one.
var ErrGitNotFound = errors.New("gitops: git was not found on PATH")

// ErrVersionTooOld means a git binary was found and its version could be
// read, but it is older than MinimumVersion.
var ErrVersionTooOld = errors.New("gitops: git version is older than the minimum supported")

// Binary is a git executable that has been located and version-checked. Every
// method that talks to git goes through the exact path recorded here, so a
// Binary obtained from Locate is the only handle this package hands out for
// running git commands.
type Binary struct {
	// Path is the resolved, absolute (or PATH-resolved) location of the git
	// executable, as returned by exec.LookPath.
	Path string

	// Version is the parsed result of `git --version` for this binary.
	Version Version
}

// Locate finds git on PATH (see DefaultBinaryName) and checks that its
// version is at least MinimumVersion. Out-of-range or unreadable versions are
// reported as a clear, typed error — never a silent guess about what the
// binary can do.
func Locate(ctx context.Context) (*Binary, error) {
	return LocateNamed(ctx, DefaultBinaryName)
}

// LocateNamed behaves like Locate but resolves name instead of
// DefaultBinaryName. It exists so tests can point at a stand-in executable
// without mutating PATH for "git" itself; production code should call Locate.
func LocateNamed(ctx context.Context, name string) (*Binary, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrGitNotFound, err)
	}
	return newBinary(ctx, path)
}

// newBinary runs `<path> --version`, parses it, and enforces MinimumVersion.
func newBinary(ctx context.Context, path string) (*Binary, error) {
	stdout, _, err := runRaw(ctx, path, "", "--version")
	if err != nil {
		return nil, fmt.Errorf("gitops: running %q --version: %w", path, err)
	}

	version, err := parseVersionOutput(stdout)
	if err != nil {
		return nil, err
	}

	if version.Less(MinimumVersion) {
		return nil, fmt.Errorf("%w: found %s at %s, need at least %s",
			ErrVersionTooOld, version, path, MinimumVersion)
	}

	return &Binary{Path: path, Version: version}, nil
}

// run executes a git subcommand through this binary. args must be built from
// literal strings, never from concatenating a caller-supplied string into a
// single token: see runRaw for the safety property this relies on.
func (b *Binary) run(ctx context.Context, dir string, args ...string) (stdout string, stderr string, err error) {
	return runRaw(ctx, b.Path, dir, args...)
}
