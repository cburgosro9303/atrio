// Command fakegit is a controllable stand-in for the git binary, used only by
// gitops tests. It is not part of the module's build (it lives under
// testdata, which `go build ./...`, `go vet ./...` and `go list ./...` all
// skip) and never touches a real repository.
//
// Its behavior is driven entirely by environment variables so tests can
// simulate a chosen `--version` output, a captured argument list, or a chosen
// exit code without needing a specific real git version installed on the
// machine running the tests:
//
//   - FAKEGIT_VERSION_OUTPUT: if set and the first argument is "--version",
//     this exact string is written to stdout and the process exits 0.
//   - FAKEGIT_ARGV_DUMP: if set to a file path, the process arguments
//     (os.Args[1:]) are written to that file as a JSON array, byte for byte,
//     before anything else runs. This is what proves an argument containing
//     shell metacharacters reaches the process unchanged.
//   - FAKEGIT_STDOUT: if set, written verbatim to stdout.
//   - FAKEGIT_EXIT_CODE: if set to an integer, used as the process exit code
//     (default 0).
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

func main() {
	os.Exit(run())
}

func run() int {
	if out := os.Getenv("FAKEGIT_VERSION_OUTPUT"); out != "" && len(os.Args) >= 2 && os.Args[1] == "--version" {
		fmt.Fprint(os.Stdout, out) //nolint:errcheck // best-effort write to a pipe the test controls
		return 0
	}

	if dumpPath := os.Getenv("FAKEGIT_ARGV_DUMP"); dumpPath != "" {
		data, err := json.Marshal(os.Args[1:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err) //nolint:errcheck // best-effort diagnostic on a path tests already fail
			return 1
		}
		if err := os.WriteFile(dumpPath, data, 0o600); err != nil { //nolint:gosec // dumpPath is a test-controlled temp file
			fmt.Fprintln(os.Stderr, err) //nolint:errcheck // best-effort diagnostic on a path tests already fail
			return 1
		}
	}

	if out := os.Getenv("FAKEGIT_STDOUT"); out != "" {
		fmt.Fprint(os.Stdout, out) //nolint:errcheck // best-effort write to a pipe the test controls
	}

	code := 0
	if raw := os.Getenv("FAKEGIT_EXIT_CODE"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			code = n
		}
	}
	return code
}
