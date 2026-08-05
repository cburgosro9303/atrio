package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// fakeIdentity is a deterministic Identity for tests: no git binary, no
// environment, just the two values the test asserts against.
type fakeIdentity struct {
	name, email string
	err         error
}

func (f fakeIdentity) Name() (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.name, nil
}

func (f fakeIdentity) Email() (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.email, nil
}

var testIdentity = fakeIdentity{name: "Cesar Burgos", email: "cesar@example.com"}

func newTestRepository(t *testing.T) *Repository {
	t.Helper()

	repo, err := Open(t.TempDir(), testIdentity)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return repo
}

// findField reports whether fields contains an entry naming field.
func findField(fields []FieldError, field string) (FieldError, bool) {
	for _, f := range fields {
		if f.Field == field {
			return f, true
		}
	}
	return FieldError{}, false
}

func fieldNames(fields []FieldError) []string {
	names := make([]string, len(fields))
	for i, f := range fields {
		names[i] = f.Field
	}
	return names
}

// requireRejected asserts err is an *ArtifactValidationError naming field.
// Most call sites only need the assertion; requireValidationError below is
// for the few that also want to inspect the returned error further. The two
// are separate functions, rather than one calling the other, because
// *ArtifactValidationError implements error and this project's errcheck
// settings (check-blank, check-type-assertions) reject a bare call whose
// result goes unused, error-shaped return values included.
func requireRejected(t *testing.T, err error, field string) {
	t.Helper()

	verr := validationErrorOrFatal(t, err)
	if _, ok := findField(verr.Fields, field); !ok {
		t.Fatalf("rejection does not name %q\nfields: %v\nfull error: %v", field, fieldNames(verr.Fields), verr)
	}
}

// requireValidationError asserts err is an *ArtifactValidationError naming
// field, and returns it for further inspection.
func requireValidationError(t *testing.T, err error, field string) *ArtifactValidationError {
	t.Helper()

	verr := validationErrorOrFatal(t, err)
	if _, ok := findField(verr.Fields, field); !ok {
		t.Fatalf("rejection does not name %q\nfields: %v\nfull error: %v", field, fieldNames(verr.Fields), verr)
	}
	return verr
}

func validationErrorOrFatal(t *testing.T, err error) *ArtifactValidationError {
	t.Helper()

	var verr *ArtifactValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("got error %T (%v), want *ArtifactValidationError", err, err)
	}
	return verr
}

// --- minimal, schema-valid business documents, one per kind ---

func validActor() Document {
	return Document{"kind": "human", "name": "Cesar Burgos", "email": "cesar@example.com"}
}

func validTaskBusiness() Document {
	return Document{
		"type":  "task",
		"title": "Write the thing",
		"state": "draft",
		"stateHistory": []any{
			Document{"to": "draft", "by": validActor(), "at": "2026-01-01T00:00:00Z"},
		},
	}
}

func validDecisionBusiness() Document {
	return Document{
		"title":        "Use ULIDs",
		"context":      "Artifacts need sortable, collision-resistant ids.",
		"decision":     "Use github.com/oklog/ulid/v2.",
		"consequences": []any{"IDs sort chronologically."},
		"status":       "active",
	}
}

func validLogEntryBusiness() Document {
	return Document{
		"eventType": "note",
		"summary":   "Something happened.",
	}
}

func validChangelogBusiness(taskID string) Document {
	return Document{
		"taskRefs": []any{taskID},
		"branch":   "task/01abc-slug",
		"summary":  "Did the thing.",
		"changes": []any{
			Document{"path": "a.go", "kind": "added", "description": "new file"},
		},
	}
}

func validFlowProgressBusiness() Document {
	return Document{
		"flowRef": "init-flow@1.0.0",
		"stages": []any{
			Document{"id": "explore", "state": "pending"},
		},
	}
}

func validProjectConfigBusiness() Document {
	return Document{
		"name":             "Atrio",
		"description":      "A project.",
		"artifactLanguage": "es",
		"environments":     []any{"dev"},
		"layers":           []any{},
		"providers":        []any{Document{"id": "claude-code"}},
		"platformVersion":  "0.1.0",
		"schemaVersions":   Document{"task": 1},
	}
}

func validAgentsBusiness() Document {
	return Document{
		"agents": []any{
			Document{
				"ref":             "architect@1.0.0",
				"personalization": Document{"displayName": "Andres"},
			},
		},
	}
}

// writeRawFile writes raw bytes directly to path, bypassing the repository —
// standing in for a hand-edited or externally-corrupted file on disk.
func writeRawFile(t *testing.T, path string, content []byte) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("creating %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
