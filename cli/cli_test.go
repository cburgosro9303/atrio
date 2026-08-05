package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// failingWriter reports a write error on every call, to exercise the paths
// where the command line cannot reach its output stream.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestExecuteReportsVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if code := Execute([]string{"atrio"}, &stdout, &stderr); code != exitOK {
		t.Errorf("exit code = %d, want %d", code, exitOK)
	}
	if got, want := stdout.String(), "atrio "+Version+"\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

func TestExecuteRejectsUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if code := Execute([]string{"atrio", "init"}, &stdout, &stderr); code != exitUsage {
		t.Errorf("exit code = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "init") {
		t.Errorf("stderr = %q, want it to name the rejected command", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
}

func TestExecuteReportsWriteFailures(t *testing.T) {
	tests := map[string][]string{
		"version on stdout":   {"atrio"},
		"rejection on stderr": {"atrio", "init"},
	}

	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			// Both streams fail, so whichever one the branch writes to trips.
			if code := Execute(args, failingWriter{}, failingWriter{}); code != exitWriteError {
				t.Errorf("exit code = %d, want %d", code, exitWriteError)
			}
		})
	}
}
