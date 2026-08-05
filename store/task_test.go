package store

import (
	"errors"
	"testing"
)

var errUnconfiguredIdentity = errors.New("git identity is not configured")

func TestCreateAndReadTask_RoundTrip(t *testing.T) {
	repo := newTestRepository(t)

	id, created, err := repo.CreateTask(validTaskBusiness())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if !isWellFormedULID(id) {
		t.Fatalf("CreateTask returned malformed id %q", id)
	}
	if created["title"] != "Write the thing" {
		t.Fatalf("created document lost a business field: %v", created)
	}

	read, err := repo.ReadTask(id)
	if err != nil {
		t.Fatalf("ReadTask: %v", err)
	}
	if read["title"] != "Write the thing" || read["id"] != id {
		t.Fatalf("round trip mismatch: wrote %v, read %v", created, read)
	}
}

func TestCreateTask_CreatedByFromInjectedIdentity(t *testing.T) {
	identity := fakeIdentity{name: "Ada Lovelace", email: "ada@example.com"}
	repo, err := Open(t.TempDir(), identity)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	_, doc, err := repo.CreateTask(validTaskBusiness())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// CreateTask returns the document exactly as a later ReadTask would
	// decode it (repository.go's createArtifact re-reads what it just
	// wrote), so createdBy comes back as a plain map[string]any here, not a
	// Document — asMap accepts both, which is the point of it existing.
	createdBy, ok := asMap(doc["createdBy"])
	if !ok {
		t.Fatalf("createdBy is not an object: %#v", doc["createdBy"])
	}
	if createdBy["kind"] != "human" || createdBy["name"] != "Ada Lovelace" || createdBy["email"] != "ada@example.com" {
		t.Fatalf("createdBy was not populated from the injected identity: %v", createdBy)
	}
}

func TestCreateTask_RejectsMalformedIdentityEmail(t *testing.T) {
	// createdBy.email carries the schema's "format": "email", and format
	// assertions are off by default in the underlying library for drafts
	// 2019-09+ (validate.go's compileSchemas turns them on for this
	// package specifically). This is the regression that decision guards:
	// without AssertFormat, a git identity with a broken email would be
	// written and read back as if nothing were wrong.
	repo, err := Open(t.TempDir(), fakeIdentity{name: "Cesar", email: "not-an-email"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	_, _, err = repo.CreateTask(validTaskBusiness())
	requireRejected(t, err, "createdBy/email")
}

func TestCreateTask_IdentityFailurePropagates(t *testing.T) {
	repo, err := Open(t.TempDir(), fakeIdentity{err: errUnconfiguredIdentity})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if _, _, err := repo.CreateTask(validTaskBusiness()); err == nil {
		t.Fatal("CreateTask succeeded despite a failing identity")
	}
}

func TestUpdateTask_PreservesEnvelopeIdentity(t *testing.T) {
	repo := newTestRepository(t)

	id, created, err := repo.CreateTask(validTaskBusiness())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	history, ok := created["stateHistory"].([]any)
	if !ok || len(history) == 0 {
		t.Fatalf("created task has no stateHistory: %v", created)
	}

	patch := validTaskBusiness()
	patch["state"] = "ready_for_dev"
	patch["stateHistory"] = []any{
		history[0],
		Document{"from": "draft", "to": "ready_for_dev", "by": validActor(), "at": "2026-01-02T00:00:00Z"},
	}

	updated, err := repo.UpdateTask(id, patch)
	if err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}

	if updated["id"] != created["id"] {
		t.Errorf("id changed across update: %v -> %v", created["id"], updated["id"])
	}
	if updated["createdAt"] != created["createdAt"] {
		t.Errorf("createdAt changed across update: %v -> %v", created["createdAt"], updated["createdAt"])
	}
	if updated["updatedAt"] == created["updatedAt"] {
		t.Errorf("updatedAt did not move on update")
	}
	if updated["state"] != "ready_for_dev" {
		t.Errorf("business field did not take: state = %v", updated["state"])
	}
}

func TestCreateTask_RejectsMissingBusinessField(t *testing.T) {
	repo := newTestRepository(t)

	business := validTaskBusiness()
	delete(business, "title")

	_, _, err := repo.CreateTask(business)
	requireRejected(t, err, "title")
}

func TestCreateTask_RejectsInvalidEnumValue(t *testing.T) {
	repo := newTestRepository(t)

	business := validTaskBusiness()
	business["state"] = "not-a-real-state"

	_, _, err := repo.CreateTask(business)
	requireRejected(t, err, "state")
}

func TestCreateTask_RejectsUnknownProperty(t *testing.T) {
	repo := newTestRepository(t)

	business := validTaskBusiness()
	business["totallyMadeUp"] = true

	_, _, err := repo.CreateTask(business)
	requireRejected(t, err, "totallyMadeUp")
}

func TestCreateTask_RejectsBlockedWithoutBlockedBy(t *testing.T) {
	repo := newTestRepository(t)

	business := validTaskBusiness()
	business["state"] = "blocked"

	_, _, err := repo.CreateTask(business)
	requireRejected(t, err, "blockedBy")
}

func TestReadTask_RejectsEnvelopeFieldNamedDirectly(t *testing.T) {
	repo := newTestRepository(t)

	// A hand-edited (or externally corrupted) file missing an envelope
	// field: createdAt, part of the envelope every artifact composes with
	// allOf, not a business field. This is the cascade case T-010's own
	// suite flagged: the failed allOf branch drags a false-schema rejection
	// behind it for the other envelope fields, and the rejection must still
	// single out createdAt as the real cause.
	id := "01J8Z3K2M4N5P6Q7R8S9T0V1W2"
	path := repo.artifactPath(taskKind, id)
	writeRawFile(t, path, []byte(`{
		"schemaVersion": 1,
		"id": "01J8Z3K2M4N5P6Q7R8S9T0V1W2",
		"updatedAt": "2026-01-01T00:00:00Z",
		"createdBy": {"kind": "human", "name": "Cesar", "email": "cesar@example.com"},
		"type": "task",
		"title": "T",
		"state": "draft",
		"stateHistory": [{"to": "draft", "by": {"kind": "human", "name": "Cesar", "email": "cesar@example.com"}, "at": "2026-01-01T00:00:00Z"}]
	}`))

	_, err := repo.ReadTask(id)
	verr := requireValidationError(t, err, "createdAt")

	// The cascade this field's own failure drags along must not smuggle in
	// a complaint about a field that is perfectly fine.
	if _, ok := findField(verr.Fields, "updatedAt"); ok {
		t.Errorf("rejection also names updatedAt, which is present and valid: %v", verr.Fields)
	}
	if _, ok := findField(verr.Fields, "createdBy"); ok {
		t.Errorf("rejection also names createdBy, which is present and valid: %v", verr.Fields)
	}
}

func TestReadTask_RejectsMalformedCreatedAt(t *testing.T) {
	repo := newTestRepository(t)

	// createdAt carries "format": "date-time" and no pattern backup
	// (common.schema.json). The schemas package's own suite never checks
	// this — format assertions are off by default in the underlying
	// library for draft 2019-09+, and schemas/schemas_test.go compiles with
	// that default — so this package's compiler is the only place that
	// turns AssertFormat on (store/validate.go) and therefore the only
	// place this ever gets exercised. A string that is present, of the
	// right JSON type, and simply not a real date has to be rejected by
	// name, not silently accepted as "close enough".
	id := "01J8Z3K2M4N5P6Q7R8S9T0V1W2"
	path := repo.artifactPath(taskKind, id)
	writeRawFile(t, path, []byte(`{
		"schemaVersion": 1,
		"id": "01J8Z3K2M4N5P6Q7R8S9T0V1W2",
		"createdAt": "yesterday",
		"updatedAt": "2026-01-01T00:00:00Z",
		"createdBy": {"kind": "human", "name": "Cesar", "email": "cesar@example.com"},
		"type": "task",
		"title": "T",
		"state": "draft",
		"stateHistory": [{"to": "draft", "by": {"kind": "human", "name": "Cesar", "email": "cesar@example.com"}, "at": "2026-01-01T00:00:00Z"}]
	}`))

	_, err := repo.ReadTask(id)
	requireRejected(t, err, "createdAt")
}
