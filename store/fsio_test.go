package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteNew_PrimaryRoute(t *testing.T) {
	path := filepath.Join(t.TempDir(), "entry.json")

	if err := writeNew(path, []byte(`{"a":1}`)); err != nil {
		t.Fatalf("writeNew: %v", err)
	}

	got, err := os.ReadFile(path) //nolint:gosec // path built from t.TempDir()
	if err != nil {
		t.Fatalf("reading written file: %v", err)
	}
	if string(got) != `{"a":1}` {
		t.Fatalf("content mismatch: got %q", got)
	}
}

func TestWriteNew_PrimaryRouteRefusesExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "entry.json")

	if err := writeNew(path, []byte(`{"a":1}`)); err != nil {
		t.Fatalf("first writeNew: %v", err)
	}

	err := writeNew(path, []byte(`{"a":2}`))
	if err == nil {
		t.Fatal("second writeNew succeeded against an existing file")
	}
	var verr *ArtifactValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("got %T, want *ArtifactValidationError", err)
	}
}

// errLinkUnsupported stands in for whatever a filesystem without hard-link
// support reports (FAT32/exFAT, some network shares): anything that is not
// os.ErrExist. os.Link succeeds on ext4, APFS and NTFS — the filesystems
// behind every CI runner this project has — so without forcing a failure
// here, writeNew's fallback route would carry zero coverage on any platform
// the test suite actually runs on.
var errLinkUnsupported = errors.New("hard links not supported on this filesystem")

func TestWriteNew_FallsBackWhenLinkIsUnsupported(t *testing.T) {
	original := linkFile
	linkFile = func(string, string) error { return errLinkUnsupported }
	t.Cleanup(func() { linkFile = original })

	path := filepath.Join(t.TempDir(), "entry.json")

	if err := writeNew(path, []byte(`{"a":1}`)); err != nil {
		t.Fatalf("writeNew with unsupported hard links: %v", err)
	}

	got, err := os.ReadFile(path) //nolint:gosec // path built from t.TempDir()
	if err != nil {
		t.Fatalf("reading written file: %v", err)
	}
	if string(got) != `{"a":1}` {
		t.Fatalf("content mismatch: got %q", got)
	}
}

func TestWriteNew_FallbackRefusesExisting(t *testing.T) {
	original := linkFile
	linkFile = func(string, string) error { return errLinkUnsupported }
	t.Cleanup(func() { linkFile = original })

	path := filepath.Join(t.TempDir(), "entry.json")

	if err := writeNew(path, []byte(`{"a":1}`)); err != nil {
		t.Fatalf("first writeNew (fallback route): %v", err)
	}

	// The O_EXCL claim is what has to catch this collision now that Link
	// itself is forced to fail for an unrelated reason on every call: this
	// is the scenario a stat-then-rename fallback could lose to a race, and
	// O_EXCL cannot, because there is no gap between observing the name is
	// free and claiming it.
	err := writeNew(path, []byte(`{"a":2}`))
	if err == nil {
		t.Fatal("second writeNew (fallback route) succeeded against an existing file")
	}
	var verr *ArtifactValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("got %T, want *ArtifactValidationError", err)
	}

	got, err := os.ReadFile(path) //nolint:gosec // path built from t.TempDir()
	if err != nil {
		t.Fatalf("reading the file after the refused overwrite: %v", err)
	}
	if string(got) != `{"a":1}` {
		t.Fatalf("the first write was modified by the refused second one: got %q", got)
	}
}
