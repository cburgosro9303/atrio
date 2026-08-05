package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// readDocument reads and decodes one artifact file. A missing file becomes
// a *NotFoundError and unparseable content becomes a *CorruptArtifactError;
// callers that need schema validation on top of this call it separately, so
// the two failure modes never get mixed into one message that has to guess
// which problem it is looking at.
func readDocument(path string) (Document, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is built from a validated ulid under this repository's own root
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, &NotFoundError{Path: path}
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	// jsonschema.UnmarshalJSON decodes numbers into json.Number rather than
	// float64, and rejects trailing bytes after the top-level value — which
	// is what turns a file truncated mid-write into a reported error here
	// instead of a silently short document.
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, &CorruptArtifactError{Path: path, Err: err}
	}
	doc, ok := value.(map[string]any)
	if !ok {
		return nil, &CorruptArtifactError{Path: path, Err: fmt.Errorf("top-level JSON value is a %T, not an object", value)}
	}
	return Document(doc), nil
}

// encode renders a document as indented, deterministic JSON: encoding/json
// sorts map keys, so byte-identical content always produces byte-identical
// output, which keeps a git diff of a re-saved-but-unchanged artifact empty.
func encode(doc Document) ([]byte, error) {
	data, err := json.MarshalIndent(map[string]any(doc), "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encoding artifact: %w", err)
	}
	return append(data, '\n'), nil
}

// writeTemp writes data to a fresh temporary file in dir, fsyncs it and
// closes it, and returns its path. It never touches the artifact's real
// path, which is what lets both callers below build "no half-written
// artifact ever lands at its real name" on top of it: a crash here leaves
// only a stray temp file, never a truncated one at the name anything reads.
func writeTemp(dir string, data []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("creating %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return "", fmt.Errorf("creating temp file in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()

	// Every cleanup error below is folded into the one returned via
	// errors.Join rather than discarded: a failed Close or Remove here is
	// itself worth knowing about (a stray temp file left behind, or a
	// filesystem in a bad state), not merely noise on top of the write
	// failure that triggered the cleanup.
	if _, err := tmp.Write(data); err != nil {
		return "", fmt.Errorf("writing %s: %w", tmpPath, errors.Join(err, tmp.Close(), os.Remove(tmpPath)))
	}
	if err := tmp.Sync(); err != nil {
		return "", fmt.Errorf("syncing %s: %w", tmpPath, errors.Join(err, tmp.Close(), os.Remove(tmpPath)))
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("closing %s: %w", tmpPath, errors.Join(err, os.Remove(tmpPath)))
	}
	return tmpPath, nil
}

// writeReplacing atomically writes data to path, replacing whatever was
// there. Used for every mutable artifact (task, decision, changelog,
// flow-progress, and the two singletons): the content lands fully formed via
// rename, an operation the OS performs as a single step, so a crash mid-write
// leaves either the previous version or nothing new — never a half-written
// file at path.
func writeReplacing(path string, data []byte) error {
	tmpPath, err := writeTemp(filepath.Dir(path), data)
	if err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("renaming %s to %s: %w", tmpPath, path, errors.Join(err, os.Remove(tmpPath)))
	}
	return nil
}

// linkFile is os.Link behind a package variable so a test can force the
// primary path of writeNew to fail without needing a filesystem that
// genuinely lacks hard-link support (ext4, APFS and NTFS — the three
// platforms CI runs on — all have it, so that branch would otherwise carry
// zero coverage on every runner this project has).
var linkFile = os.Link

// writeNew atomically writes data to path, refusing if path already exists.
// It is what append-only artifacts (a log entry) are built on.
//
// The primary route is a hard link: unlike rename, which on every platform
// Go supports replaces an existing destination, a hard link fails atomically
// with "file exists" instead — so two writers racing for the same name can
// never make one silently clobber the other, which a
// stat-then-write-then-rename sequence could still lose to a race. It is
// also the most reliable primitive on older NFS, which is precisely why it
// stays the primary route rather than being replaced outright.
//
// Not every filesystem supports hard links (FAT32/exFAT, some network
// shares), so a Link failure that is not ErrExist falls back to a second
// atomic route rather than to stat-then-rename: claiming path itself with
// O_EXCL. That either wins the name outright or fails with ErrExist — there
// is no window between "observe" and "act" for two racing writers to both
// fall through, unlike a Stat check followed by a separate Rename. FAT32 and
// exFAT both support O_EXCL. The data itself is written and fsynced to the
// temp name first in both routes, so the same crash-safety writeReplacing
// gives every other artifact applies here too.
func writeNew(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmpPath, err := writeTemp(dir, data)
	if err != nil {
		return err
	}
	defer os.Remove(tmpPath) //nolint:errcheck // best-effort cleanup of the temp name; the link (or, on the fallback below, the rename) is what matters

	linkErr := linkFile(tmpPath, path)
	if linkErr == nil {
		return nil
	}
	if errors.Is(linkErr, os.ErrExist) {
		return appendOnlyViolation(path)
	}

	claim, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //nolint:gosec // path is built from a validated ulid under this repository's own root, same as readDocument
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return appendOnlyViolation(path)
		}
		return fmt.Errorf("claiming %s: %w", path, errors.Join(linkErr, err))
	}
	if err := claim.Close(); err != nil {
		return fmt.Errorf("closing claim on %s: %w", path, errors.Join(linkErr, err))
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("linking %s to %s: %w", tmpPath, path, errors.Join(linkErr, err))
	}
	return nil
}

func appendOnlyViolation(path string) error {
	return &ArtifactValidationError{
		Path: path,
		Fields: []FieldError{{
			Field:  "id",
			Reason: "an artifact already exists at this id; the log is append-only and an entry is never overwritten",
		}},
	}
}
