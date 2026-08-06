package store

import (
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTestNotifications opens a fresh LocalDB in a temp directory (mirroring
// newOpenedLocalDB in localdb_test.go) and returns a Notifications backed by
// it. Tests that need raw database access reach it directly through the
// unexported db field — this file lives in package store, same as
// localdb_test.go.
func newTestNotifications(t *testing.T) *Notifications {
	t.Helper()

	ldb, _ := newOpenedLocalDB(t)
	return NewNotifications(ldb)
}

// insertRawNotification writes a notification row bypassing Notifications
// entirely, standing in for a row already on disk before this package wrote
// it — e.g. a corrupt created_at, or a value from a foreign or older writer.
// Named distinctly from localdb_test.go's insertNotification, which writes
// category = "general", a value outside this file's closed Go catalog.
func insertRawNotification(t *testing.T, db *sql.DB, id, createdAt, level, category, title string) {
	t.Helper()

	if _, err := db.Exec(
		`insert into notification (id, created_at, level, category, title) values (?, ?, ?, ?, ?)`,
		id, createdAt, level, category, title,
	); err != nil {
		t.Fatalf("inserting raw notification %s: %v", id, err)
	}
}

func mustNewULID(t *testing.T) string {
	t.Helper()

	id, err := newIDGenerator().next()
	if err != nil {
		t.Fatalf("generating ulid: %v", err)
	}
	return id
}

func notificationIDs(notifications []Notification) []string {
	ids := make([]string, len(notifications))
	for i, n := range notifications {
		ids[i] = n.ID
	}
	return ids
}

func containsID(ids []string, id string) bool {
	for _, got := range ids {
		if got == id {
			return true
		}
	}
	return false
}

// requireNotificationFieldError asserts err is a *NotificationValidationError
// naming field, reusing testing_test.go's findField/fieldNames — they operate
// on []FieldError, exactly what NotificationValidationError.Fields is.
func requireNotificationFieldError(t *testing.T, err error, field string) {
	t.Helper()

	var verr *NotificationValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("got error %T (%v), want *NotificationValidationError", err, err)
	}
	if _, ok := findField(verr.Fields, field); !ok {
		t.Fatalf("rejection does not name %q\nfields: %v\nfull error: %v", field, fieldNames(verr.Fields), verr)
	}
}

// --- round trip ---

// TestNotify_RoundTripsThroughList catches Notify returning something other
// than what a List call right after it would produce — the same contract
// T-020's Create/Update/Write already promise for JSON artifacts
// (04-backlog-m1.md:120).
func TestNotify_RoundTripsThroughList(t *testing.T) {
	notifications := newTestNotifications(t)

	created, err := notifications.Notify(LevelWarning, CategoryFlow, "Stage stuck", "closing extraction failed")
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if created.ReadAt != nil {
		t.Fatalf("Notify().ReadAt = %v, want nil", created.ReadAt)
	}

	got, err := notifications.List(NotificationFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("List() returned %d notifications, want 1", len(got))
	}
	if !reflect.DeepEqual(got[0], created) {
		t.Fatalf("List()[0] = %+v, want %+v (Notify's own return value)", got[0], created)
	}
}

// TestNotify_UsesInjectedIDAndClock catches the ID not coming out
// well-formed, and CreatedAt not reflecting the injected clock rather than
// wall time.
func TestNotify_UsesInjectedIDAndClock(t *testing.T) {
	notifications := newTestNotifications(t)
	frozen := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	notifications.now = func() time.Time { return frozen }

	notif, err := notifications.Notify(LevelError, CategoryMigration, "Format changed", "")
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if !isWellFormedULID(notif.ID) {
		t.Fatalf("Notify().ID = %q is not a well-formed ULID", notif.ID)
	}
	if !notif.CreatedAt.Equal(frozen) {
		t.Fatalf("Notify().CreatedAt = %v, want %v", notif.CreatedAt, frozen)
	}
}

// --- ordering ---

// TestList_OrdersByCreatedAtDescThenIDDesc catches the id-descending tiebreak
// being missing or wrong. The clock is frozen so every row shares the exact
// same created_at, isolating the tiebreak: id generation is deliberately
// left on the real clock, since ulid.Monotonic is what guarantees ids minted
// in sequence sort strictly increasing, which is what makes "newest first"
// stable across runs instead of depending on map/slice iteration order.
func TestList_OrdersByCreatedAtDescThenIDDesc(t *testing.T) {
	notifications := newTestNotifications(t)
	frozen := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	notifications.now = func() time.Time { return frozen }

	var ids []string
	for i := range 5 {
		notif, err := notifications.Notify(LevelInfo, CategoryDocument, fmt.Sprintf("n%d", i), "")
		if err != nil {
			t.Fatalf("Notify #%d: %v", i, err)
		}
		if !notif.CreatedAt.Equal(frozen) {
			t.Fatalf("Notify #%d CreatedAt = %v, want %v", i, notif.CreatedAt, frozen)
		}
		ids = append(ids, notif.ID)
	}

	got, err := notifications.List(NotificationFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != len(ids) {
		t.Fatalf("List() returned %d notifications, want %d", len(got), len(ids))
	}
	for i, notif := range got {
		want := ids[len(ids)-1-i]
		if notif.ID != want {
			t.Fatalf("List()[%d].ID = %q, want %q (descending id order with tied created_at)", i, notif.ID, want)
		}
	}
}

// --- filters ---

func TestList_EmptyFilterMatchesEverything(t *testing.T) {
	notifications := newTestNotifications(t)
	a, err := notifications.Notify(LevelInfo, CategoryDocument, "a", "")
	if err != nil {
		t.Fatalf("Notify a: %v", err)
	}
	b, err := notifications.Notify(LevelError, CategoryMigration, "b", "")
	if err != nil {
		t.Fatalf("Notify b: %v", err)
	}

	got, err := notifications.List(NotificationFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	ids := notificationIDs(got)
	if len(ids) != 2 || !containsID(ids, a.ID) || !containsID(ids, b.ID) {
		t.Fatalf("List(empty filter) = %v, want both %s and %s", ids, a.ID, b.ID)
	}
}

// TestList_FiltersByMinLevel catches the boundary being wrong in either
// direction: warning must include error and exclude info.
func TestList_FiltersByMinLevel(t *testing.T) {
	notifications := newTestNotifications(t)

	info, err := notifications.Notify(LevelInfo, CategoryDocument, "info", "")
	if err != nil {
		t.Fatalf("Notify info: %v", err)
	}
	warn, err := notifications.Notify(LevelWarning, CategoryDocument, "warn", "")
	if err != nil {
		t.Fatalf("Notify warn: %v", err)
	}
	errN, err := notifications.Notify(LevelError, CategoryDocument, "error", "")
	if err != nil {
		t.Fatalf("Notify error: %v", err)
	}

	got, err := notifications.List(NotificationFilter{MinLevel: LevelWarning})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	ids := notificationIDs(got)
	if containsID(ids, info.ID) {
		t.Fatalf("MinLevel=warning included an info notification: %v", ids)
	}
	if !containsID(ids, warn.ID) || !containsID(ids, errN.ID) {
		t.Fatalf("MinLevel=warning excluded warning or error: %v", ids)
	}
}

func TestList_FiltersByCategories(t *testing.T) {
	notifications := newTestNotifications(t)

	doc, err := notifications.Notify(LevelInfo, CategoryDocument, "doc", "")
	if err != nil {
		t.Fatalf("Notify doc: %v", err)
	}
	flow, err := notifications.Notify(LevelInfo, CategoryFlow, "flow", "")
	if err != nil {
		t.Fatalf("Notify flow: %v", err)
	}
	if _, err := notifications.Notify(LevelInfo, CategoryCatalog, "catalog", ""); err != nil {
		t.Fatalf("Notify catalog: %v", err)
	}

	got, err := notifications.List(NotificationFilter{
		Categories: []NotificationCategory{CategoryDocument, CategoryFlow},
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	ids := notificationIDs(got)
	if len(ids) != 2 || !containsID(ids, doc.ID) || !containsID(ids, flow.ID) {
		t.Fatalf("List(Categories: [document, flow]) = %v, want %s and %s only", ids, doc.ID, flow.ID)
	}
}

func TestList_FiltersByUnreadOnly(t *testing.T) {
	notifications := newTestNotifications(t)

	unread, err := notifications.Notify(LevelInfo, CategoryDocument, "unread", "")
	if err != nil {
		t.Fatalf("Notify unread: %v", err)
	}
	read, err := notifications.Notify(LevelInfo, CategoryDocument, "read", "")
	if err != nil {
		t.Fatalf("Notify read: %v", err)
	}
	if err := notifications.MarkRead(read.ID); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}

	got, err := notifications.List(NotificationFilter{UnreadOnly: true})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	ids := notificationIDs(got)
	if containsID(ids, read.ID) {
		t.Fatalf("UnreadOnly included a read notification: %v", ids)
	}
	if !containsID(ids, unread.ID) {
		t.Fatalf("UnreadOnly excluded an unread notification: %v", ids)
	}
}

func TestList_FiltersByLimit(t *testing.T) {
	notifications := newTestNotifications(t)

	var ids []string
	for i := range 5 {
		notif, err := notifications.Notify(LevelInfo, CategoryDocument, fmt.Sprintf("n%d", i), "")
		if err != nil {
			t.Fatalf("Notify #%d: %v", i, err)
		}
		ids = append(ids, notif.ID)
	}

	got, err := notifications.List(NotificationFilter{Limit: 2})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []string{ids[4], ids[3]}
	gotIDs := notificationIDs(got)
	if !reflect.DeepEqual(gotIDs, want) {
		t.Fatalf("List(Limit: 2) ids = %v, want %v (newest first)", gotIDs, want)
	}
}

// TestList_CombinesFilters exercises MinLevel, Categories and UnreadOnly
// together, not just each in isolation.
func TestList_CombinesFilters(t *testing.T) {
	notifications := newTestNotifications(t)

	mustNotify := func(level NotificationLevel, category NotificationCategory, title string) Notification {
		t.Helper()
		notif, err := notifications.Notify(level, category, title, "")
		if err != nil {
			t.Fatalf("Notify(%s): %v", title, err)
		}
		return notif
	}

	mustNotify(LevelInfo, CategoryDocument, "excluded: below MinLevel")
	wantA := mustNotify(LevelWarning, CategoryDocument, "matches")
	mustNotify(LevelWarning, CategoryFlow, "excluded: wrong category")
	wantB := mustNotify(LevelError, CategoryDocument, "matches, unread")
	readOne := mustNotify(LevelError, CategoryDocument, "excluded: already read")

	if err := notifications.MarkRead(readOne.ID); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}

	got, err := notifications.List(NotificationFilter{
		MinLevel:   LevelWarning,
		Categories: []NotificationCategory{CategoryDocument},
		UnreadOnly: true,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	gotIDs := notificationIDs(got)
	wantIDs := []string{wantB.ID, wantA.ID} // newest first
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("List(combined filter) ids = %v, want %v", gotIDs, wantIDs)
	}
}

// --- validation ---

func TestNotify_RejectsUnknownLevel(t *testing.T) {
	notifications := newTestNotifications(t)
	_, err := notifications.Notify("urgent", CategoryDocument, "t", "")
	requireNotificationFieldError(t, err, "level")
}

func TestNotify_RejectsUnknownCategory(t *testing.T) {
	notifications := newTestNotifications(t)
	_, err := notifications.Notify(LevelInfo, "bogus", "t", "")
	requireNotificationFieldError(t, err, "category")
}

func TestNotify_RejectsEmptyTitle(t *testing.T) {
	notifications := newTestNotifications(t)
	_, err := notifications.Notify(LevelInfo, CategoryDocument, "", "")
	requireNotificationFieldError(t, err, "title")
}

func TestList_RejectsUnknownMinLevel(t *testing.T) {
	notifications := newTestNotifications(t)
	_, err := notifications.List(NotificationFilter{MinLevel: "urgent"})
	requireNotificationFieldError(t, err, "minLevel")
}

func TestList_RejectsUnknownCategoryInFilter(t *testing.T) {
	notifications := newTestNotifications(t)
	_, err := notifications.List(NotificationFilter{Categories: []NotificationCategory{"bogus"}})
	requireNotificationFieldError(t, err, "categories/0")
}

func TestList_RejectsNegativeLimit(t *testing.T) {
	notifications := newTestNotifications(t)
	_, err := notifications.List(NotificationFilter{Limit: -1})
	requireNotificationFieldError(t, err, "limit")
}

// --- MarkRead ---

// TestMarkRead_MarksAndIsIdempotent catches read_at not being stamped, and
// catches a second MarkRead moving an already-stamped read_at forward — the
// clock advances between the two calls specifically so a re-stamp would be
// observable.
func TestMarkRead_MarksAndIsIdempotent(t *testing.T) {
	notifications := newTestNotifications(t)
	notifications.now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

	created, err := notifications.Notify(LevelInfo, CategoryDocument, "n", "")
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}

	firstRead := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	notifications.now = func() time.Time { return firstRead }
	if err := notifications.MarkRead(created.ID); err != nil {
		t.Fatalf("first MarkRead: %v", err)
	}

	got, err := notifications.List(NotificationFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].ReadAt == nil || !got[0].ReadAt.Equal(firstRead) {
		t.Fatalf("after first MarkRead: got %+v, want ReadAt = %v", got, firstRead)
	}

	secondRead := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	notifications.now = func() time.Time { return secondRead }
	if err := notifications.MarkRead(created.ID); err != nil {
		t.Fatalf("second MarkRead: %v", err)
	}

	got, err = notifications.List(NotificationFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].ReadAt == nil || !got[0].ReadAt.Equal(firstRead) {
		t.Fatalf("second MarkRead moved ReadAt: got %+v, want unchanged %v", got, firstRead)
	}
}

func TestMarkRead_FailsNamingNonexistentID(t *testing.T) {
	notifications := newTestNotifications(t)
	missing := mustNewULID(t)

	err := notifications.MarkRead(missing)
	if err == nil {
		t.Fatal("MarkRead: want error for a nonexistent id, got nil")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Fatalf("error %q does not name the missing id %q", err, missing)
	}
}

// TestMarkRead_NonexistentIDInBatchLeavesOthersUnmarked catches a partial
// commit: if one id in a batch does not exist, none of the others in the
// same call may end up marked read either.
func TestMarkRead_NonexistentIDInBatchLeavesOthersUnmarked(t *testing.T) {
	notifications := newTestNotifications(t)

	a, err := notifications.Notify(LevelInfo, CategoryDocument, "a", "")
	if err != nil {
		t.Fatalf("Notify a: %v", err)
	}
	b, err := notifications.Notify(LevelInfo, CategoryDocument, "b", "")
	if err != nil {
		t.Fatalf("Notify b: %v", err)
	}
	missing := mustNewULID(t)

	err = notifications.MarkRead(a.ID, missing, b.ID)
	if err == nil {
		t.Fatal("MarkRead: want error for a batch containing a nonexistent id, got nil")
	}

	got, err := notifications.List(NotificationFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, notif := range got {
		if notif.ReadAt != nil {
			t.Fatalf("notification %s was marked read despite the batch failing: ReadAt = %v", notif.ID, notif.ReadAt)
		}
	}
}

// TestMarkRead_RejectsMalformedIDWithoutTouchingDatabase catches the
// malformed-id check running after some database work already happened.
//
// Asserting "nothing got marked" is not a real discriminator here: a
// malformed id caught later, inside the transaction, would also leave
// nothing marked once the deferred Rollback ran — the observable state is
// identical either way (the same shape of gap T-021 named and fixed by
// renaming TestOpenLocalDB_RebuildsAlongsideStaleWALSidecars,
// 04-backlog-m1.md:165). What actually tells "checked before" apart from
// "checked after, then rolled back" is a database that cannot be touched at
// all: this test closes the LocalDB handle first, so any database access —
// even one that would itself fail and roll back — surfaces as "sql: database
// is closed" instead of the format-rejection error. The malformed-id error
// coming back clean, with no mention of a closed database, is the proof the
// validation loop runs before db.Begin.
func TestMarkRead_RejectsMalformedIDWithoutTouchingDatabase(t *testing.T) {
	ldb, err := OpenLocalDB(t.TempDir())
	if err != nil {
		t.Fatalf("OpenLocalDB: %v", err)
	}
	if err := ldb.Close(); err != nil {
		t.Fatalf("closing LocalDB: %v", err)
	}

	err = NewNotifications(ldb).MarkRead("not-a-ulid")
	if err == nil {
		t.Fatal("MarkRead: want error for a malformed id, got nil")
	}
	if !strings.Contains(err.Error(), "not-a-ulid") {
		t.Fatalf("error %q does not name the malformed id", err)
	}
	if strings.Contains(err.Error(), "database is closed") {
		t.Fatalf("MarkRead reached the (closed) database before validating the id: %v", err)
	}
}

func TestMarkRead_NoOpWithZeroIDs(t *testing.T) {
	notifications := newTestNotifications(t)
	if err := notifications.MarkRead(); err != nil {
		t.Fatalf("MarkRead with no ids: %v", err)
	}
}

// --- corruption ---

// TestList_FailsExplicitlyOnCorruptCreatedAt catches List returning a zero
// time.Time (or otherwise succeeding) for a row whose created_at is not
// parseable RFC 3339 — silently swallowing that as if the field had simply
// never been read.
func TestList_FailsExplicitlyOnCorruptCreatedAt(t *testing.T) {
	notifications := newTestNotifications(t)
	insertRawNotification(t, notifications.db.DB(), mustNewULID(t), "not-a-timestamp", string(LevelInfo), string(CategoryDocument), "n")

	if _, err := notifications.List(NotificationFilter{}); err == nil {
		t.Fatal("List: want error for a corrupt created_at, got nil")
	}
}

// TestList_TreatsUnknownStoredValuesAsReadable pins down two claims made in
// notification.go's comments that no other test exercises, because every
// other test only ever writes through Notify, which never produces an
// out-of-catalog level or category: (1) a row whose level/category is
// outside today's Go catalog is still returned by List rather than rejected
// — writes are closed, reads are tolerant — and (2) an empty MinLevel really
// issues no level predicate (the row surfaces), while a non-empty MinLevel
// really does filter via `level in (...)` (the same row disappears, because
// an out-of-catalog level cannot be "at least" any known one).
func TestList_TreatsUnknownStoredValuesAsReadable(t *testing.T) {
	notifications := newTestNotifications(t)
	id := mustNewULID(t)
	insertRawNotification(t, notifications.db.DB(), id, "2026-01-01T00:00:00Z", "critical", "unknown-category", "n")

	all, err := notifications.List(NotificationFilter{})
	if err != nil {
		t.Fatalf("List(empty filter): %v", err)
	}
	if len(all) != 1 || all[0].ID != id {
		t.Fatalf("List(empty filter) = %v, want the out-of-catalog row %s to be readable", notificationIDs(all), id)
	}
	if all[0].Level != "critical" || all[0].Category != "unknown-category" {
		t.Fatalf("List returned Level=%q Category=%q, want the raw stored values unchanged", all[0].Level, all[0].Category)
	}

	filtered, err := notifications.List(NotificationFilter{MinLevel: LevelInfo})
	if err != nil {
		t.Fatalf("List(MinLevel: info): %v", err)
	}
	if len(filtered) != 0 {
		t.Fatalf("List(MinLevel: info) = %v, want the out-of-catalog level excluded", notificationIDs(filtered))
	}
}

// --- concurrency ---

// TestNotifications_ConcurrentUse gives -race something to look at: several
// goroutines writing and reading through the same Notifications value, which
// never caches db.DB() (see the type's doc comment) and shares one
// idGenerator across calls.
func TestNotifications_ConcurrentUse(t *testing.T) {
	notifications := newTestNotifications(t)

	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			notif, err := notifications.Notify(LevelInfo, CategoryDocument, fmt.Sprintf("worker %d", i), "")
			if err != nil {
				errs <- fmt.Errorf("Notify: %w", err)
				return
			}
			if _, err := notifications.List(NotificationFilter{}); err != nil {
				errs <- fmt.Errorf("List: %w", err)
				return
			}
			if err := notifications.MarkRead(notif.ID); err != nil {
				errs <- fmt.Errorf("MarkRead: %w", err)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	got, err := notifications.List(NotificationFilter{})
	if err != nil {
		t.Fatalf("final List: %v", err)
	}
	if len(got) != workers {
		t.Fatalf("List() returned %d notifications, want %d", len(got), workers)
	}
	for _, notif := range got {
		if notif.ReadAt == nil {
			t.Fatalf("notification %s was not marked read", notif.ID)
		}
	}
}
