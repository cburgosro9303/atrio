package cli

import (
	"fmt"
	"io"
)

// Process exit codes returned by Execute.
const (
	exitOK         = 0
	exitUsage      = 2
	exitWriteError = 3
)

// Version is the build version of the binary. Release builds override it with
// -ldflags; development builds keep the placeholder.
var Version = "dev"

// Execute runs the atrio command line and returns the process exit code.
//
// The command set is introduced in T-080; for now this reports the build
// version, which is enough to prove the binary links and runs.
func Execute(args []string, stdout, stderr io.Writer) int {
	if len(args) > 1 {
		if _, err := fmt.Fprintf(stderr, "atrio: no commands available yet (see task T-080): %s\n", args[1]); err != nil {
			return exitWriteError
		}
		return exitUsage
	}

	if _, err := fmt.Fprintf(stdout, "atrio %s\n", Version); err != nil {
		return exitWriteError
	}
	return exitOK
}
