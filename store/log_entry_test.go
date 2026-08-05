package store

import (
	"testing"
	"time"
)

func TestCreateAndReadLogEntry_RoundTrip(t *testing.T) {
	repo := newTestRepository(t)

	id, created, err := repo.CreateLogEntry(validLogEntryBusiness())
	if err != nil {
		t.Fatalf("CreateLogEntry: %v", err)
	}

	read, err := repo.ReadLogEntry(id)
	if err != nil {
		t.Fatalf("ReadLogEntry: %v", err)
	}
	if read["summary"] != created["summary"] {
		t.Fatalf("round trip mismatch: wrote %v, read %v", created, read)
	}
}

func TestCreateLogEntry_AuthorizationRequiresPayload(t *testing.T) {
	repo := newTestRepository(t)

	business := Document{
		"eventType": "authorization_granted",
		"summary":   "granted network access",
	}

	_, _, err := repo.CreateLogEntry(business)
	requireRejected(t, err, "payload")
}

func TestCreateLogEntry_AppendOnly_RefusesToOverwrite(t *testing.T) {
	repo := newTestRepository(t)

	// Force two creations to collide on the same id: the ULID generator is
	// overridden with one that always returns the same value, standing in
	// for the vanishingly unlikely real-world case log-entry.schema.json
	// itself calls out — two entries created concurrently, offline, that
	// happen to collide. The store's promise is that even then, the second
	// write never silently clobbers the first.
	const fixedID = "01J8Z3K2M4N5P6Q7R8S9T0V1W2"
	repo.newID = func() (string, error) { return fixedID, nil }

	firstID, first, err := repo.CreateLogEntry(validLogEntryBusiness())
	if err != nil {
		t.Fatalf("first CreateLogEntry: %v", err)
	}
	if firstID != fixedID {
		t.Fatalf("test setup broken: got id %q, want %q", firstID, fixedID)
	}

	second := validLogEntryBusiness()
	second["summary"] = "a different event, same id"
	_, _, err = repo.CreateLogEntry(second)
	if err == nil {
		t.Fatal("second CreateLogEntry with a colliding id succeeded; the log must be append-only")
	}
	requireRejected(t, err, "id")

	// The first entry must be exactly what it was: untouched by the failed
	// second attempt.
	read, err := repo.ReadLogEntry(fixedID)
	if err != nil {
		t.Fatalf("ReadLogEntry after the refused overwrite: %v", err)
	}
	if read["summary"] != first["summary"] {
		t.Fatalf("the first entry was modified by the refused overwrite: now %v", read)
	}
}

func TestListLogEntryIDs_ChronologicalOrder(t *testing.T) {
	repo := newTestRepository(t)

	var ids []string
	for i := 0; i < 5; i++ {
		id, _, err := repo.CreateLogEntry(validLogEntryBusiness())
		if err != nil {
			t.Fatalf("CreateLogEntry #%d: %v", i, err)
		}
		ids = append(ids, id)
		// A tiny pause is not required for ULIDs to sort correctly within
		// the same millisecond (that is the point of the monotonic entropy
		// source, exercised by TestULID_MonotonicWithinSameMillisecond) —
		// this is only here to make the test's own intent unambiguous: ids
		// created later must list later even across millisecond boundaries.
		time.Sleep(time.Millisecond)
	}

	listed, err := repo.ListLogEntryIDs()
	if err != nil {
		t.Fatalf("ListLogEntryIDs: %v", err)
	}
	if len(listed) != len(ids) {
		t.Fatalf("got %d ids, want %d", len(listed), len(ids))
	}
	for i, want := range ids {
		if listed[i] != want {
			t.Fatalf("listed[%d] = %s, want %s (listed: %v)", i, listed[i], want, listed)
		}
	}
}
