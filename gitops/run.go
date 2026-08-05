package gitops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// runError reports the failure of a single git invocation. It never carries
// the command as a single string: Args is preserved as the argument slice
// that was actually passed to exec.CommandContext, which is what makes it
// possible to tell a caller exactly what ran without reconstructing a shell
// command line that was never built in the first place.
type runError struct {
	// path is the resolved binary that was executed, e.g. "/usr/bin/git".
	path string

	// args is the git subcommand and its arguments, exactly as given to
	// exec.CommandContext — never concatenated into a string.
	args []string

	dir    string
	stderr string
	err    error
}

func (e *runError) Error() string {
	cmd := strings.Join(append([]string{e.path}, e.args...), " ")
	if e.stderr != "" {
		return fmt.Sprintf("gitops: %s: %v: %s", cmd, e.err, e.stderr)
	}
	return fmt.Sprintf("gitops: %s: %v", cmd, e.err)
}

func (e *runError) Unwrap() error { return e.err }

// exitCode returns the process exit code, or -1 if the process never
// produced one (e.g. the binary could not be started at all).
func (e *runError) exitCode() int {
	var exitErr *exec.ExitError
	if errors.As(e.err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

// runRaw executes path with args as an argument array — the one form this
// package uses to invoke external processes. It never builds a command
// through string concatenation and never involves a shell: exec.CommandContext
// passes path and each element of args to the operating system directly, so a
// value containing ";", "$(...)", quotes or spaces reaches the process
// unchanged and is never interpreted as shell syntax.
//
// dir sets the process working directory; an empty dir leaves it unset, which
// exec.CommandContext already treats as "the current process's directory".
//
// cmd.Env is deliberately left nil, which makes exec.CommandContext inherit
// the calling process's environment as-is. That is what lets tests isolate
// git's config lookup with t.Setenv (see isolateGitConfig in the test files),
// but it also means ambient GIT_DIR / GIT_WORK_TREE / GIT_INDEX_FILE
// variables, if set in the environment atrio itself runs in, could redirect a
// git invocation away from dir. Known and left open for whichever task first
// needs a directory guarantee stronger than "no shell interpolation" — worktree
// management (T-031) is the natural owner of that decision.
func runRaw(ctx context.Context, path string, dir string, args ...string) (stdout string, stderr string, err error) {
	// G204: path and args are supplied by this package's own callers (a
	// binary located by Locate/LocateNamed, and literal subcommand strings
	// this file writes) — never by an external caller of the public API. The
	// argument-array form below is exactly the safe pattern the project's
	// no-interpolated-shell rule requires; see run_test.go for a test that
	// exercises it with shell metacharacters and confirms nothing is
	// reinterpreted.
	cmd := exec.CommandContext(ctx, path, args...) //nolint:gosec // constant call shape, argument array, no shell
	cmd.Dir = dir

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	runErr := cmd.Run()
	stdout = outBuf.String()
	stderr = errBuf.String()
	if runErr != nil {
		return stdout, stderr, &runError{
			path:   path,
			args:   args,
			dir:    dir,
			stderr: strings.TrimSpace(stderr),
			err:    runErr,
		}
	}
	return stdout, stderr, nil
}
