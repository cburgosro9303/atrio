package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
)

// schemaTables are the 9 named tables a fresh bootstrap (or a rebuild) must
// produce: the 8 real ones plus the FTS5 virtual table document_fts. None of
// the shadow tables FTS5 creates alongside document_fts are included.
var schemaTables = []string{
	"document", "document_tag", "document_relation", "document_issue",
	"materialization", "notification", "session", "project_lock",
	"document_fts",
}

// realSchemaTables is schemaTables without the virtual document_fts table:
// the 8 real tables T-021 classifies as either derivable or ephemeral.
var realSchemaTables = []string{
	"document", "document_tag", "document_relation", "document_issue",
	"materialization", "notification", "session", "project_lock",
}

// TestOpenLocalDB_BootstrapsAFreshDatabase catches a fresh project not
// getting the complete schema in one shot: every real table, the FTS5
// virtual table, the delete trigger, and the current generation stamp.
func TestOpenLocalDB_BootstrapsAFreshDatabase(t *testing.T) {
	root := t.TempDir()

	ldb, err := OpenLocalDB(root)
	if err != nil {
		t.Fatalf("OpenLocalDB: %v", err)
	}
	t.Cleanup(func() {
		if err := ldb.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	wantPath := filepath.Join(root, ".atrio", "atrio.db")
	if ldb.Path() != wantPath {
		t.Fatalf("Path() = %q, want %q", ldb.Path(), wantPath)
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("database file missing at %s: %v", wantPath, err)
	}

	if got := pragmaInt(t, ldb.DB(), "user_version"); got != localDBGeneration {
		t.Fatalf("user_version = %d, want %d", got, localDBGeneration)
	}

	assertSameSet(t, realTableNames(t, ldb.DB()), realSchemaTables)
	if !hasTable(t, ldb.DB(), "document_fts") {
		t.Fatal("document_fts virtual table is missing")
	}
	assertSameSet(t, triggerNames(t, ldb.DB()), []string{"document_fts_after_delete"})

	// No assertion on the total number of objects in sqlite_master. The shadow
	// tables FTS5 creates for document_fts are its own implementation detail,
	// free to change between SQLite releases, and pinning their count would
	// make this test fail on an upgrade that broke nothing — the same reason
	// T-010 refused to write fixture counts into prose. The assertions above
	// name what this schema actually promises.
}

// TestOpenLocalDB_CreatesTheAtrioDirectory catches OpenLocalDB assuming
// .atrio already exists: a project that has never written any artifact yet
// has no such directory to piggyback on.
func TestOpenLocalDB_CreatesTheAtrioDirectory(t *testing.T) {
	root := t.TempDir()
	atrioPath := filepath.Join(root, ".atrio")
	if _, err := os.Stat(atrioPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("test setup: %s already exists", atrioPath)
	}

	ldb, err := OpenLocalDB(root)
	if err != nil {
		t.Fatalf("OpenLocalDB: %v", err)
	}
	t.Cleanup(func() {
		if err := ldb.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	info, err := os.Stat(atrioPath)
	if err != nil {
		t.Fatalf("stat %s after OpenLocalDB: %v", atrioPath, err)
	}
	if !info.IsDir() {
		t.Fatalf("%s exists but is not a directory", atrioPath)
	}
}

// TestOpenLocalDB_ReopeningKeepsData catches OpenLocalDB rebuilding an
// up-to-date database on every open: it must discard and recreate only when
// the stored generation differs, never unconditionally.
func TestOpenLocalDB_ReopeningKeepsData(t *testing.T) {
	root := t.TempDir()

	first, err := OpenLocalDB(root)
	if err != nil {
		t.Fatalf("first OpenLocalDB: %v", err)
	}
	insertNotification(t, first.DB(), "n1")
	if err := first.Close(); err != nil {
		t.Fatalf("closing first handle: %v", err)
	}

	second, err := OpenLocalDB(root)
	if err != nil {
		t.Fatalf("second OpenLocalDB: %v", err)
	}
	t.Cleanup(func() {
		if err := second.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	if count := countRows(t, second.DB(), "notification"); count != 1 {
		t.Fatalf("notification rows after reopening = %d, want 1", count)
	}
}

// TestOpenLocalDB_RebuildsOnForeignGeneration catches a generation this
// build did not write being migrated in place or silently accepted instead
// of being discarded and rebuilt from scratch — see localDBGeneration for
// why migration is never the right move here.
func TestOpenLocalDB_RebuildsOnForeignGeneration(t *testing.T) {
	root := t.TempDir()

	first, err := OpenLocalDB(root)
	if err != nil {
		t.Fatalf("first OpenLocalDB: %v", err)
	}
	insertNotification(t, first.DB(), "n1")
	if _, err := first.DB().Exec(`pragma user_version = 99`); err != nil {
		t.Fatalf("stamping a foreign generation: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("closing first handle: %v", err)
	}

	second, err := OpenLocalDB(root)
	if err != nil {
		t.Fatalf("second OpenLocalDB: %v", err)
	}
	t.Cleanup(func() {
		if err := second.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	if got := pragmaInt(t, second.DB(), "user_version"); got != localDBGeneration {
		t.Fatalf("user_version after rebuild = %d, want %d", got, localDBGeneration)
	}
	assertSameSet(t, schemaTableNames(t, second.DB()), schemaTables)
	if count := countRows(t, second.DB(), "notification"); count != 0 {
		t.Fatalf("notification rows after rebuild = %d, want 0", count)
	}
}

// TestOpenLocalDB_DiscardsAnUnstampedDatabase catches user_version 0 — the
// same value SQLite gives an untouched file — being treated as "already
// current" instead of "not ours, discard and rebuild."
func TestOpenLocalDB_DiscardsAnUnstampedDatabase(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".atrio", "atrio.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("creating %s: %v", filepath.Dir(path), err)
	}

	raw, err := sql.Open(localDBDriver, "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatalf("opening a raw database at %s: %v", path, err)
	}
	if _, err := raw.Exec(`create table foreign_table (x integer)`); err != nil {
		t.Fatalf("creating a foreign table: %v", err)
	}
	// Closed before OpenLocalDB runs: discardAndBootstrap removes this file,
	// and an open handle on it would make that fail on platforms (Windows,
	// in this project's CI matrix) that refuse to delete an open file.
	if err := raw.Close(); err != nil {
		t.Fatalf("closing the raw handle: %v", err)
	}

	ldb, err := OpenLocalDB(root)
	if err != nil {
		t.Fatalf("OpenLocalDB: %v", err)
	}
	t.Cleanup(func() {
		if err := ldb.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	assertSameSet(t, schemaTableNames(t, ldb.DB()), schemaTables)
}

// TestOpenLocalDB_RebuildsAlongsideStaleWALSidecars catches a rebuild failing,
// or producing a wrong schema, when a crashed process left -wal/-shm sidecars
// next to the database.
//
// Named for what it checks, not for what would be nice to check. It does NOT
// prove the rebuild is what removed the sidecars, and no end-to-end test can:
// SQLite resets a -wal whose header does not match the database beside it, so
// the observable outcome is identical whether or not localDBFiles lists them.
// Verified by mutation — dropping the sidecars from localDBFiles leaves this
// test green. The file list itself is pinned by
// TestLocalDBFiles_ListsTheDatabaseAndItsSidecars.
func TestOpenLocalDB_RebuildsAlongsideStaleWALSidecars(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".atrio", "atrio.db")

	first, err := OpenLocalDB(root)
	if err != nil {
		t.Fatalf("first OpenLocalDB: %v", err)
	}
	insertNotification(t, first.DB(), "n1")
	if _, err := first.DB().Exec(`pragma user_version = 99`); err != nil {
		t.Fatalf("stamping a foreign generation: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("closing first handle: %v", err)
	}

	// A clean Close above already checkpointed and removed its own sidecars,
	// so these stand in for the ones a process that died mid-write would
	// leave behind.
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.WriteFile(path+suffix, []byte("stale wal frame"), 0o600); err != nil {
			t.Fatalf("writing fake sidecar %s: %v", path+suffix, err)
		}
	}

	second, err := OpenLocalDB(root)
	if err != nil {
		t.Fatalf("second OpenLocalDB: %v", err)
	}

	if got := pragmaInt(t, second.DB(), "user_version"); got != localDBGeneration {
		t.Fatalf("user_version after rebuild = %d, want %d", got, localDBGeneration)
	}
	assertSameSet(t, schemaTableNames(t, second.DB()), schemaTables)
	if count := countRows(t, second.DB(), "notification"); count != 0 {
		t.Fatalf("notification rows after rebuild = %d, want 0", count)
	}

	// A live WAL connection recreates -wal/-shm on its own, so the "sidecars
	// are gone" assertion only makes sense after a clean close.
	if err := second.Close(); err != nil {
		t.Fatalf("closing second handle: %v", err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(path + suffix); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("sidecar %s still present after a clean close: %v", path+suffix, err)
		}
	}
}

// TestReset_EmptiesEveryTableAndKeepsTheSchema catches Reset relying on a
// hand-written list of tables to clear, which would silently skip a table
// added later, and catches DB() not returning a usable handle after Reset
// swaps it out.
func TestReset_EmptiesEveryTableAndKeepsTheSchema(t *testing.T) {
	ldb, _ := newOpenedLocalDB(t)

	insertNotification(t, ldb.DB(), "n1")
	insertDocument(t, ldb.DB(), "d1", "docs/a.md")
	if _, err := ldb.DB().Exec(
		`insert into project_lock (scope, lockfile, holder_pid, holder_host, holder_kind, acquired_at)
		 values ('project', 'x.lock', 1, 'host', 'cli', '2026-01-01T00:00:00Z')`,
	); err != nil {
		t.Fatalf("inserting project_lock row: %v", err)
	}

	if err := ldb.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	db := ldb.DB()
	if got := pragmaInt(t, db, "user_version"); got != localDBGeneration {
		t.Fatalf("user_version after Reset = %d, want %d", got, localDBGeneration)
	}

	tables := schemaTableNames(t, db)
	assertSameSet(t, tables, schemaTables)

	for _, table := range tables {
		if count := countRows(t, db, table); count != 0 {
			t.Errorf("table %s has %d rows after Reset, want 0", table, count)
		}
	}
}

// TestLocalDBDSN_AppliesPragmasOnAwkwardPaths catches localDBDSN building the
// connection string by concatenating "?_pragma=..." onto the raw path
// instead of a proper SQLite URI. The "with?question" case is the one that
// breaks under naive concatenation: SQLite reads everything from the first
// '?' onward as query parameters, so the database ends up created at a
// truncated path and every pragma, including foreign_keys, is silently
// dropped — which means the ON DELETE CASCADEs in localdb.sql stop firing
// with no error anywhere.
func TestLocalDBDSN_AppliesPragmasOnAwkwardPaths(t *testing.T) {
	names := []struct {
		name string
		// unrepresentableOnWindows marks a directory name Windows refuses to
		// create at all. Windows reserves < > : " / \ | ? * in filenames, so
		// the '?' case cannot be set up there — os.MkdirAll fails with "the
		// filename, directory name, or volume label syntax is incorrect"
		// before OpenLocalDB is ever reached. Skipping it is not weakening the
		// test: a path that cannot exist cannot carry the defect either, and
		// the case still runs on the two platforms where it can.
		unrepresentableOnWindows bool
	}{
		{name: "plain"},
		{name: "with space"},
		{name: "with?question", unrepresentableOnWindows: true},
		{name: "with#hash"},
		{name: "with%percent"},
		{name: "ñ-acentos"},
	}

	for _, tc := range names {
		t.Run(tc.name, func(t *testing.T) {
			if tc.unrepresentableOnWindows && runtime.GOOS == "windows" {
				t.Skip("Windows reserves '?' in filenames, so this directory cannot be created")
			}

			root := filepath.Join(t.TempDir(), tc.name)
			if err := os.MkdirAll(root, 0o750); err != nil {
				t.Fatalf("creating project root %q: %v", root, err)
			}

			ldb, err := OpenLocalDB(root)
			if err != nil {
				t.Fatalf("OpenLocalDB(%q): %v", root, err)
			}
			t.Cleanup(func() {
				if err := ldb.Close(); err != nil {
					t.Errorf("Close: %v", err)
				}
			})

			wantPath := filepath.Join(root, ".atrio", "atrio.db")
			if ldb.Path() != wantPath {
				t.Fatalf("Path() = %q, want %q", ldb.Path(), wantPath)
			}
			if _, err := os.Stat(wantPath); err != nil {
				t.Fatalf("database file missing at %s: %v", wantPath, err)
			}

			if got := pragmaInt(t, ldb.DB(), "foreign_keys"); got != 1 {
				t.Fatalf("foreign_keys = %d, want 1", got)
			}
			if got := pragmaInt(t, ldb.DB(), "busy_timeout"); got != 5000 {
				t.Fatalf("busy_timeout = %d, want 5000", got)
			}
		})
	}
}

// TestLocalDB_PragmasApplyToEveryPooledConnection catches the DSN pragmas
// only reaching whichever connection happens to serve the next query, rather
// than every connection in the pool: PRAGMA state is per-connection, so
// without baking foreign_keys into the DSN itself, a pooled connection that
// never ran a one-off "pragma foreign_keys=on" would run with foreign keys
// off, and the ON DELETE CASCADEs would silently do nothing on it.
func TestLocalDB_PragmasApplyToEveryPooledConnection(t *testing.T) {
	ldb, _ := newOpenedLocalDB(t)
	ldb.DB().SetMaxOpenConns(4)

	ctx := context.Background()
	conn1, err := ldb.DB().Conn(ctx)
	if err != nil {
		t.Fatalf("acquiring first pooled connection: %v", err)
	}
	defer func() {
		if err := conn1.Close(); err != nil {
			t.Errorf("closing first connection: %v", err)
		}
	}()
	// conn1 must stay open while conn2 is acquired, or the pool would just
	// hand back the same connection and this test would prove nothing about
	// "every" connection.
	conn2, err := ldb.DB().Conn(ctx)
	if err != nil {
		t.Fatalf("acquiring second pooled connection: %v", err)
	}
	defer func() {
		if err := conn2.Close(); err != nil {
			t.Errorf("closing second connection: %v", err)
		}
	}()

	for i, conn := range []*sql.Conn{conn1, conn2} {
		var fk int
		if err := conn.QueryRowContext(ctx, `pragma foreign_keys`).Scan(&fk); err != nil {
			t.Fatalf("reading foreign_keys on connection %d: %v", i, err)
		}
		if fk != 1 {
			t.Fatalf("connection %d: foreign_keys = %d, want 1", i, fk)
		}
	}

	if _, err := conn1.ExecContext(ctx,
		`insert into document (id, path, title, purpose, language, content_sha256, indexed_at)
		 values (?, ?, ?, ?, ?, ?, ?)`,
		"d1", "docs/a.md", "Title", "Purpose", "en", "sha", "2026-01-01T00:00:00Z",
	); err != nil {
		t.Fatalf("inserting document on connection 1: %v", err)
	}
	if _, err := conn1.ExecContext(ctx,
		`insert into document_tag (document_id, tag) values (?, ?)`, "d1", "x",
	); err != nil {
		t.Fatalf("inserting document_tag on connection 1: %v", err)
	}

	if _, err := conn2.ExecContext(ctx, `delete from document where id = ?`, "d1"); err != nil {
		t.Fatalf("deleting document on connection 2: %v", err)
	}

	var tagCount int
	if err := conn1.QueryRowContext(ctx,
		`select count(*) from document_tag where document_id = ?`, "d1",
	).Scan(&tagCount); err != nil {
		t.Fatalf("counting surviving tags: %v", err)
	}
	if tagCount != 0 {
		t.Fatalf("document_tag rows after deleting the parent document on another connection = %d, want 0 "+
			"(the cascade did not fire, meaning that connection ran without foreign_keys on)", tagCount)
	}
}

// TestLocalDB_JournalModeIsWAL catches the journal_mode pragma silently
// failing to apply. Unlike the per-connection pragmas, journal_mode is a
// property of the file itself, so a single connection's readback is enough.
func TestLocalDB_JournalModeIsWAL(t *testing.T) {
	ldb, _ := newOpenedLocalDB(t)

	var mode string
	if err := ldb.DB().QueryRow(`pragma journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("reading journal_mode: %v", err)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Fatalf("journal_mode = %q, want %q", mode, "wal")
	}
}

// TestLocalDBSchema_StrictTablesRejectWrongTypes catches a missing STRICT
// keyword: without it SQLite stores a string in an INTEGER column as-is,
// which is exactly the silent corruption a derived cache cannot afford,
// since a rebuild would just keep reproducing it.
func TestLocalDBSchema_StrictTablesRejectWrongTypes(t *testing.T) {
	ldb, _ := newOpenedLocalDB(t)

	_, err := ldb.DB().Exec(
		`insert into project_lock (scope, lockfile, holder_pid, holder_host, holder_kind, acquired_at)
		 values (?, ?, ?, ?, ?, ?)`,
		"project", "x.lock", "not-a-pid", "host", "cli", "2026-01-01T00:00:00Z",
	)
	if err == nil {
		t.Fatal("insert with a non-integer holder_pid succeeded")
	}
	if !strings.Contains(err.Error(), "holder_pid") {
		t.Fatalf("error does not name the offending column: %v", err)
	}
}

// TestLocalDBSchema_RelationKindCatalogIsClosed catches a missing or
// loosened CHECK constraint: without it, a typo'd relation kind becomes an
// unqueryable value sitting in a table no JSON Schema ever validates.
func TestLocalDBSchema_RelationKindCatalogIsClosed(t *testing.T) {
	ldb, _ := newOpenedLocalDB(t)
	insertDocument(t, ldb.DB(), "d1", "docs/a.md")

	_, err := ldb.DB().Exec(
		`insert into document_relation (document_id, kind, target_type, target_id) values (?, ?, ?, ?)`,
		"d1", "bogus", "task", "t1",
	)
	if err == nil {
		t.Fatal("insert with an out-of-catalog relation kind succeeded")
	}
}

// TestLocalDBSchema_DeletingADocumentClearsItsSearchRow catches the delete
// trigger and the ON DELETE CASCADEs drifting apart: forgetting either one
// leaves a phantom search hit, or orphaned tag/relation rows, pointing at a
// document that no longer exists.
func TestLocalDBSchema_DeletingADocumentClearsItsSearchRow(t *testing.T) {
	ldb, _ := newOpenedLocalDB(t)
	insertDocument(t, ldb.DB(), "d1", "docs/a.md")

	if _, err := ldb.DB().Exec(
		`insert into document_fts (document_id, title, purpose, body) values (?, ?, ?, ?)`,
		"d1", "Title", "Purpose", "some searchable body",
	); err != nil {
		t.Fatalf("inserting document_fts row: %v", err)
	}
	if _, err := ldb.DB().Exec(
		`insert into document_tag (document_id, tag) values (?, ?)`, "d1", "x",
	); err != nil {
		t.Fatalf("inserting document_tag row: %v", err)
	}
	if _, err := ldb.DB().Exec(
		`insert into document_relation (document_id, kind, target_type, target_id) values (?, ?, ?, ?)`,
		"d1", "relates_to", "task", "t1",
	); err != nil {
		t.Fatalf("inserting document_relation row: %v", err)
	}

	if _, err := ldb.DB().Exec(`delete from document where id = ?`, "d1"); err != nil {
		t.Fatalf("deleting the document: %v", err)
	}

	if count := countRows(t, ldb.DB(), "document_tag"); count != 0 {
		t.Errorf("document_tag rows after delete = %d, want 0", count)
	}
	if count := countRows(t, ldb.DB(), "document_relation"); count != 0 {
		t.Errorf("document_relation rows after delete = %d, want 0", count)
	}

	var ftsCount int
	if err := ldb.DB().QueryRow(
		`select count(*) from document_fts where document_id = ?`, "d1",
	).Scan(&ftsCount); err != nil {
		t.Fatalf("counting document_fts rows: %v", err)
	}
	if ftsCount != 0 {
		t.Errorf("document_fts rows after delete = %d, want 0 (the delete trigger did not fire)", ftsCount)
	}
}

// TestLocalDBSchema_SearchLinkageIsByIdentityNotRowid catches document_fts
// being keyed on document.rowid instead of document.id.
//
// `document` declares a TEXT primary key and so has no INTEGER PRIMARY KEY
// alias, which SQLite documents as leaving VACUUM free to renumber its rowids.
// Testing that through VACUUM alone is not enough — this SQLite build happens
// not to renumber, so a rowid-keyed trigger survives it. What does force the
// distinction is inserting the search rows in a different order than the
// documents: FTS5 then assigns rowids that do not line up, and a rowid-keyed
// trigger deletes some other document's search row. Verified by mutation:
// switching the trigger back to `where rowid = old.rowid` turns this red.
func TestLocalDBSchema_SearchLinkageIsByIdentityNotRowid(t *testing.T) {
	ldb, _ := newOpenedLocalDB(t)

	ids := []string{"d1", "d2", "d3"}
	for i, id := range ids {
		insertDocument(t, ldb.DB(), id, fmt.Sprintf("docs/%d.md", i))
	}
	// Rotated on purpose: d1's document row is rowid 1, but its search row is
	// the last one inserted. Nothing obliges the indexer to write them in
	// lockstep, and the schema must not depend on it.
	for _, id := range []string{"d2", "d3", "d1"} {
		if _, err := ldb.DB().Exec(
			`insert into document_fts (document_id, title, purpose, body) values (?, ?, ?, ?)`,
			id, "Title", "Purpose", "body belonging to "+id,
		); err != nil {
			t.Fatalf("inserting document_fts row for %s: %v", id, err)
		}
	}

	if _, err := ldb.DB().Exec(`delete from document where id = ?`, "d1"); err != nil {
		t.Fatalf("deleting d1: %v", err)
	}
	// VACUUM as well, since closing the gap is the other half of the claim.
	if _, err := ldb.DB().Exec(`vacuum`); err != nil {
		t.Fatalf("vacuum: %v", err)
	}

	var orphaned int
	if err := ldb.DB().QueryRow(
		`select count(*) from document_fts where document_id = ?`, "d1",
	).Scan(&orphaned); err != nil {
		t.Fatalf("counting d1 search rows: %v", err)
	}
	if orphaned != 0 {
		t.Errorf("d1 still has %d search rows after being deleted", orphaned)
	}

	// The survivors must be exactly the other two, still joined to their own
	// text. A rowid-keyed trigger removes one of these instead of d1's.
	for _, id := range []string{"d2", "d3"} {
		var body string
		if err := ldb.DB().QueryRow(
			`select f.body from document d join document_fts f on f.document_id = d.id where d.id = ?`, id,
		).Scan(&body); err != nil {
			t.Fatalf("joining %s to its search row: %v", id, err)
		}
		if want := "body belonging to " + id; body != want {
			t.Errorf("%s is joined to %q, want %q", id, body, want)
		}
	}
}

// TestLocalDBSchema_DocumentIDIsNotSearchable catches document_id losing its
// UNINDEXED marker. Indexing the ULID would let it be matched as a term, so a
// full-text query could be satisfied by an identifier nobody typed as prose.
func TestLocalDBSchema_DocumentIDIsNotSearchable(t *testing.T) {
	ldb, _ := newOpenedLocalDB(t)

	// A token that appears only in the identifier column, never in the text.
	const onlyInTheIDColumn = "zzqqxxid"
	if _, err := ldb.DB().Exec(
		`insert into document_fts (document_id, title, purpose, body) values (?, ?, ?, ?)`,
		onlyInTheIDColumn, "Title", "Purpose", "prose sharing nothing with the identifier",
	); err != nil {
		t.Fatalf("inserting document_fts row: %v", err)
	}

	var count int
	if err := ldb.DB().QueryRow(
		`select count(*) from document_fts where document_fts match ?`, onlyInTheIDColumn,
	).Scan(&count); err != nil {
		t.Fatalf("running the match query: %v", err)
	}
	if count != 0 {
		t.Errorf("matching a term present only in document_id returned %d hits, want 0", count)
	}

	// Still stored and readable, which is the reason the column exists.
	var got string
	if err := ldb.DB().QueryRow(
		`select document_id from document_fts where document_fts match ?`, "identifier",
	).Scan(&got); err != nil {
		t.Fatalf("reading document_id back: %v", err)
	}
	if got != onlyInTheIDColumn {
		t.Errorf("document_id = %q, want %q", got, onlyInTheIDColumn)
	}
}

// TestLocalDBSchema_DocumentPathIsUnique catches a missing UNIQUE
// constraint on document.path: two index rows claiming the same file would
// make "which one is stale?" unanswerable.
func TestLocalDBSchema_DocumentPathIsUnique(t *testing.T) {
	ldb, _ := newOpenedLocalDB(t)
	insertDocument(t, ldb.DB(), "d1", "docs/a.md")

	_, err := ldb.DB().Exec(
		`insert into document (id, path, title, purpose, language, content_sha256, indexed_at)
		 values (?, ?, ?, ?, ?, ?, ?)`,
		"d2", "docs/a.md", "Other title", "Other purpose", "en", "sha", "2026-01-01T00:00:00Z",
	)
	if err == nil {
		t.Fatal("insert with a duplicate path succeeded")
	}
}

// TestLocalDBSchema_FullTextSearchMatchesAndSnippets catches document_fts
// having been declared as a contentless FTS5 table: a contentless table
// cannot produce snippet()/highlight() at all, which would break the search
// result presentation the whole table exists to support.
func TestLocalDBSchema_FullTextSearchMatchesAndSnippets(t *testing.T) {
	ldb, _ := newOpenedLocalDB(t)

	if _, err := ldb.DB().Exec(
		`insert into document_fts (document_id, title, purpose, body) values (?, ?, ?, ?)`,
		"d1", "Atrio overview", "Explain the platform", "Atrio orchestrates agent CLIs over a monorepo.",
	); err != nil {
		t.Fatalf("inserting document_fts row: %v", err)
	}

	var count int
	if err := ldb.DB().QueryRow(
		`select count(*) from document_fts where document_fts match ?`, "orchestrates",
	).Scan(&count); err != nil {
		t.Fatalf("running the match query: %v", err)
	}
	if count != 1 {
		t.Fatalf("match count = %d, want 1", count)
	}

	// Column 3 is body: document_id(0), title(1), purpose(2), body(3).
	var snippet string
	if err := ldb.DB().QueryRow(
		`select snippet(document_fts, 3, '[', ']', '...', 8) from document_fts where document_fts match ?`,
		"orchestrates",
	).Scan(&snippet); err != nil {
		t.Fatalf("running snippet(): %v", err)
	}
	if snippet == "" {
		t.Fatal("snippet() returned an empty string")
	}
}

// TestBootstrapLocalDB_IsAtomic catches bootstrap losing its transactional
// guarantee. If it did, a process killed mid-bootstrap could leave a
// half-built database on disk, and OpenLocalDB — which trusts user_version
// alone and never inspects the tables — would accept that half-built file as
// current forever.
func TestBootstrapLocalDB_IsAtomic(t *testing.T) {
	original := localDBSchema
	localDBSchema = `create table ok (x integer) strict; create table nope(`
	t.Cleanup(func() { localDBSchema = original })

	root := t.TempDir()
	if _, err := OpenLocalDB(root); err == nil {
		t.Fatal("OpenLocalDB succeeded against a schema with a broken statement")
	}

	rawPath := filepath.Join(root, ".atrio", "atrio.db")
	raw, err := sql.Open(localDBDriver, "file:"+filepath.ToSlash(rawPath))
	if err != nil {
		t.Fatalf("opening the file left behind at %s: %v", rawPath, err)
	}
	defer func() {
		if err := raw.Close(); err != nil {
			t.Errorf("closing raw handle: %v", err)
		}
	}()

	var generation int
	if err := raw.QueryRow(`pragma user_version`).Scan(&generation); err != nil {
		t.Fatalf("reading user_version: %v", err)
	}
	if generation != 0 {
		t.Fatalf("user_version after a failed bootstrap = %d, want 0", generation)
	}

	if tables := schemaTableNames(t, raw); len(tables) != 0 {
		t.Fatalf("tables left behind after a failed bootstrap: %v", tables)
	}
}

// TestLocalDBSchema_EveryTableIsAccountedFor catches a table added to
// localdb.sql without also updating localdb.go's per-table classification
// comment — exactly the "data in the wrong store" defect ADR-006 calls out,
// because nobody would know how to repopulate that table after the T-023
// rebuild invariant.
func TestLocalDBSchema_EveryTableIsAccountedFor(t *testing.T) {
	// document_fts is deliberately absent from this map. It is conceptually
	// derivable — see LocalDB's own type comment, which groups it with the
	// document tables — but it is a virtual table, not one of the 8 real
	// ones realTableNames reports: the prefix filter that keeps FTS5's own
	// shadow tables out of that view excludes document_fts along with them.
	// Its absence here is that filter's side effect, not an oversight.
	classification := map[string]string{
		"document":          "derivable",
		"document_tag":      "derivable",
		"document_relation": "derivable",
		"document_issue":    "derivable",
		"materialization":   "derivable",
		"notification":      "ephemeral",
		"session":           "ephemeral",
		"project_lock":      "ephemeral",
	}

	ldb, _ := newOpenedLocalDB(t)
	got := realTableNames(t, ldb.DB())

	gotSet := make(map[string]bool, len(got))
	for _, name := range got {
		gotSet[name] = true
		if _, ok := classification[name]; !ok {
			t.Errorf("table %q exists in the schema but the classification map does not account for it", name)
		}
	}
	for name := range classification {
		if !gotSet[name] {
			t.Errorf("classification map lists %q but it no longer exists in the schema", name)
		}
	}
}

// TestGitignore_CoversWALSidecars catches atrio.db's WAL sidecars not being
// gitignored: WAL mode creates -wal and -shm files next to the database, and
// gitops.Status passes --untracked-files=normal on purpose so nothing stays
// hidden, so an uncovered sidecar would report every worktree dirty.
func TestGitignore_CoversWALSidecars(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", ".gitignore"))
	if err != nil {
		t.Fatalf("reading ../.gitignore: %v", err)
	}

	var patterns []string
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}

	for _, name := range []string{localDBFile, localDBFile + "-wal", localDBFile + "-shm"} {
		if !gitignoreCovers(t, patterns, name) {
			t.Errorf("no .gitignore pattern covers %s", name)
		}
	}
}

// TestLocalDB_ConcurrentUseIsRaceFree gives `go test -race` concurrent
// writers and readers to check LocalDB's own claim — the type comment on
// LocalDB promises the returned *sql.DB is safe for concurrent use.
func TestLocalDB_ConcurrentUseIsRaceFree(t *testing.T) {
	ldb, _ := newOpenedLocalDB(t)
	db := ldb.DB()

	const goroutines = 8
	const perGoroutine = 10

	var writers sync.WaitGroup
	for g := range goroutines {
		writers.Add(1)
		go func(g int) {
			defer writers.Done()
			for i := range perGoroutine {
				id := fmt.Sprintf("n-%d-%d", g, i)
				if _, err := db.Exec(
					`insert into notification (id, created_at, level, category, title) values (?, ?, ?, ?, ?)`,
					id, "2026-01-01T00:00:00Z", "info", "general", "concurrent",
				); err != nil {
					t.Errorf("goroutine %d insert %d: %v", g, i, err)
				}
			}
		}(g)
	}

	var readers sync.WaitGroup
	for range 2 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for range perGoroutine {
				var count int
				if err := db.QueryRow(`select count(*) from notification`).Scan(&count); err != nil {
					t.Errorf("concurrent read: %v", err)
				}
			}
		}()
	}

	writers.Wait()
	readers.Wait()

	if count := countRows(t, db, "notification"); count != goroutines*perGoroutine {
		t.Fatalf("notification rows = %d, want %d", count, goroutines*perGoroutine)
	}
}

// --- helpers ---

// newOpenedLocalDB opens a fresh LocalDB rooted at a t.TempDir() and closes
// it on cleanup. Tests that need to close (and possibly reopen) the handle
// themselves mid-test do not use this helper.
func newOpenedLocalDB(t *testing.T) (*LocalDB, string) {
	t.Helper()

	root := t.TempDir()
	ldb, err := OpenLocalDB(root)
	if err != nil {
		t.Fatalf("OpenLocalDB: %v", err)
	}
	t.Cleanup(func() {
		if err := ldb.Close(); err != nil {
			t.Errorf("closing LocalDB: %v", err)
		}
	})
	return ldb, root
}

// insertDocument writes a minimal, schema-valid document row.
func insertDocument(t *testing.T, db *sql.DB, id, docPath string) {
	t.Helper()

	_, err := db.Exec(
		`insert into document (id, path, title, purpose, language, content_sha256, indexed_at)
		 values (?, ?, ?, ?, ?, ?, ?)`,
		id, docPath, "Title", "Purpose", "en", "sha256hex", "2026-01-01T00:00:00Z",
	)
	if err != nil {
		t.Fatalf("inserting document %s: %v", id, err)
	}
}

// insertNotification writes a minimal, schema-valid notification row.
func insertNotification(t *testing.T, db *sql.DB, id string) {
	t.Helper()

	_, err := db.Exec(
		`insert into notification (id, created_at, level, category, title) values (?, ?, ?, ?, ?)`,
		id, "2026-01-01T00:00:00Z", "info", "general", "test notification",
	)
	if err != nil {
		t.Fatalf("inserting notification %s: %v", id, err)
	}
}

// countRows reports how many rows table currently has. table always comes
// from either a literal in this test file or from sqlite_master's own names
// fetched moments earlier in the same test, never from external input, so
// building the query by concatenation carries no injection risk here.
func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()

	var count int
	if err := db.QueryRow(`select count(*) from ` + table).Scan(&count); err != nil {
		t.Fatalf("counting rows in %s: %v", table, err)
	}
	return count
}

// pragmaInt reads an integer-valued PRAGMA. pragma is always one of this test
// file's own literal constants: SQLite does not support bind parameters
// inside a PRAGMA statement, so this cannot be expressed as a placeholder.
func pragmaInt(t *testing.T, db *sql.DB, pragma string) int {
	t.Helper()

	var value int
	if err := db.QueryRow(`pragma ` + pragma).Scan(&value); err != nil {
		t.Fatalf("reading pragma %s: %v", pragma, err)
	}
	return value
}

// hasTable reports whether name exists in db's sqlite_master as a table
// (real or virtual).
func hasTable(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()

	var count int
	if err := db.QueryRow(
		`select count(*) from sqlite_master where type = 'table' and name = ?`, name,
	).Scan(&count); err != nil {
		t.Fatalf("checking for table %s: %v", name, err)
	}
	return count > 0
}

// realTableNames lists the schema's real (non-virtual) tables: sqlite_master
// names of type 'table', excluding SQLite's own internal bookkeeping tables
// and document_fts together with every shadow table FTS5 creates for it
// (document_fts_config/_content/_data/_docsize/_idx all share that prefix).
func realTableNames(t *testing.T, db *sql.DB) []string {
	t.Helper()
	return sqliteMasterNames(t, db, "table", "document_fts")
}

// schemaTableNames lists every table this schema names, real or virtual:
// like realTableNames but keeps document_fts itself, excluding only the
// shadow tables FTS5 creates alongside it (their names all start with
// "document_fts_", with a trailing underscore document_fts itself lacks).
func schemaTableNames(t *testing.T, db *sql.DB) []string {
	t.Helper()
	return sqliteMasterNames(t, db, "table", "document_fts_")
}

// triggerNames lists every trigger name in db's sqlite_master.
func triggerNames(t *testing.T, db *sql.DB) []string {
	t.Helper()
	return sqliteMasterNames(t, db, "trigger")
}

// sqliteMasterNames lists sqlite_master names of the given type, excluding
// SQLite's own internal bookkeeping tables (sqlite_%) and any name starting
// with one of excludePrefixes.
func sqliteMasterNames(t *testing.T, db *sql.DB, kind string, excludePrefixes ...string) []string {
	t.Helper()

	rows, err := db.Query(`select name from sqlite_master where type = ?`, kind)
	if err != nil {
		t.Fatalf("querying sqlite_master for type %s: %v", kind, err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			t.Errorf("closing sqlite_master rows: %v", err)
		}
	}()

	var names []string
names:
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scanning sqlite_master row: %v", err)
		}
		if strings.HasPrefix(name, "sqlite_") {
			continue
		}
		for _, prefix := range excludePrefixes {
			if strings.HasPrefix(name, prefix) {
				continue names
			}
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating sqlite_master: %v", err)
	}
	return names
}

// assertSameSet fails the test unless got and want contain the same
// elements, ignoring order.
func assertSameSet(t *testing.T, got, want []string) {
	t.Helper()

	gotSorted := slices.Clone(got)
	wantSorted := slices.Clone(want)
	slices.Sort(gotSorted)
	slices.Sort(wantSorted)
	if !slices.Equal(gotSorted, wantSorted) {
		t.Fatalf("got %v, want %v", gotSorted, wantSorted)
	}
}

// gitignoreCovers reports whether any pattern in patterns matches name, using
// shell-style matching the way git itself does. .gitignore never contains the
// literal file names this package cares about — it covers them with patterns
// like ".atrio/*.db" and "*.db" — so a plain substring search would miss
// real coverage entirely.
func gitignoreCovers(t *testing.T, patterns []string, name string) bool {
	t.Helper()

	candidates := []string{name, ".atrio/" + name}
	for _, pattern := range patterns {
		for _, candidate := range candidates {
			ok, err := path.Match(pattern, candidate)
			if err != nil {
				t.Fatalf("invalid gitignore pattern %q: %v", pattern, err)
			}
			if ok {
				return true
			}
		}
	}
	return false
}

// TestLocalDBFiles_ListsTheDatabaseAndItsSidecars pins the set of files the
// database occupies, which is what discardAndBootstrap removes and what
// tightenLocalDBPermissions narrows.
//
// It is a direct assertion on a literal because the end-to-end behaviour
// cannot distinguish: SQLite discards a mismatched -wal on its own, so a
// rebuild that forgot the sidecars looks identical from the outside. Where a
// property has no observable consequence to assert, the honest test is the one
// that names the contract — a forgotten sidecar would still leave the
// permission tightening skipping a file that holds the same data as the
// database, which is a consequence with no test of its own either.
func TestLocalDBFiles_ListsTheDatabaseAndItsSidecars(t *testing.T) {
	const path = "/somewhere/.atrio/atrio.db"
	want := []string{path, path + "-wal", path + "-shm"}

	got := localDBFiles(path)
	if !slices.Equal(got, want) {
		t.Fatalf("localDBFiles(%q) = %v, want %v", path, got, want)
	}
}

// TestOpenLocalDB_ResolvesARelativeRoot catches the two halves of OpenLocalDB
// disagreeing about where the database lives. os.MkdirAll resolves a relative
// root against the working directory; the SQLite URI built from the same value
// is rooted at the filesystem root unless something absolutizes it first.
// Before that fix this failed outright with "unable to open database file",
// and as root it would have quietly created /relroot/.atrio/atrio.db instead.
func TestOpenLocalDB_ResolvesARelativeRoot(t *testing.T) {
	t.Chdir(t.TempDir())

	ldb, err := OpenLocalDB("relroot")
	if err != nil {
		t.Fatalf("OpenLocalDB with a relative root: %v", err)
	}
	t.Cleanup(func() {
		if err := ldb.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	want := filepath.Join(cwd, "relroot", ".atrio", "atrio.db")

	if _, err := os.Stat(want); err != nil {
		t.Fatalf("no database under the working directory at %s: %v", want, err)
	}
	// Path must name the file that actually exists, not the caller's spelling
	// of it: a Path that cannot be stat-ed is how the disagreement hid.
	if ldb.Path() != want {
		t.Fatalf("Path() = %q, want %q", ldb.Path(), want)
	}
	if got := pragmaInt(t, ldb.DB(), "user_version"); got != localDBGeneration {
		t.Fatalf("user_version = %d, want %d", got, localDBGeneration)
	}
}

// TestOpenLocalDB_RestrictsFilePermissions catches the database being left at
// the driver's default mode. store/fsio.go hands 0o600 to os.OpenFile for every
// artifact it writes; SQLite creates these files itself, so the only lever is
// narrowing them right after, and without it they land at 0644.
func TestOpenLocalDB_RestrictsFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		// os.Chmod on Windows only toggles the read-only attribute, so Unix
		// permission bits are not a property this platform has to assert.
		// Named rather than silent: the tightening still runs there.
		t.Skip("Unix permission bits are not modelled on Windows")
	}

	root := t.TempDir()
	ldb, err := OpenLocalDB(root)
	if err != nil {
		t.Fatalf("OpenLocalDB: %v", err)
	}
	t.Cleanup(func() {
		if err := ldb.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	// The sidecars WAL mode creates carry the same data as the database, so
	// checking only the database would leave the leak open.
	for _, name := range localDBFiles(ldb.Path()) {
		info, err := os.Stat(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatalf("Stat %s: %v", name, err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("%s has mode %#o, want 0600", name, perm)
		}
	}
}

// TestUserVersionStampIsTransactional pins the half of the bootstrap's
// atomicity that TestBootstrapLocalDB_IsAtomic cannot reach. That test injects
// a schema which fails during the DDL, so the stamp is never executed and its
// assertion would hold even if `pragma user_version` were not transactional at
// all. The claim in bootstrapLocalDB's comment is that the DDL and the stamp
// roll back *together*, and this is what forces the second half of it.
func TestUserVersionStampIsTransactional(t *testing.T) {
	root := t.TempDir()
	ldb, err := OpenLocalDB(root)
	if err != nil {
		t.Fatalf("OpenLocalDB: %v", err)
	}
	t.Cleanup(func() {
		if err := ldb.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	tx, err := ldb.DB().Begin()
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := tx.Exec(`pragma user_version = 42`); err != nil {
		t.Fatalf("stamping inside a transaction: %v", err)
	}
	if _, err := tx.Exec(`create table stamped_alongside (x integer) strict`); err != nil {
		t.Fatalf("creating a table alongside the stamp: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	if got := pragmaInt(t, ldb.DB(), "user_version"); got != localDBGeneration {
		t.Fatalf("user_version after rollback = %d, want %d: the stamp survived a rolled-back transaction", got, localDBGeneration)
	}
	if hasTable(t, ldb.DB(), "stamped_alongside") {
		t.Fatal("a table created in the rolled-back transaction survived")
	}
}

// TestReset_LeavesAClosedHandleWhenItFails pins the failure contract Reset
// documents. Discarding closes the handle before removing the file, so a
// failure partway has no earlier state to roll back to; what must never
// happen is the caller keeping a handle that looks alive. A closed *sql.DB
// answers every query with "sql: database is closed", and that is the signal
// this test exists to keep — if a future version reconnected on failure
// without bootstrapping, queries would start succeeding against a database
// with no schema, which is the silent version of the same bug.
func TestReset_LeavesAClosedHandleWhenItFails(t *testing.T) {
	root := t.TempDir()
	ldb, err := OpenLocalDB(root)
	if err != nil {
		t.Fatalf("OpenLocalDB: %v", err)
	}
	t.Cleanup(func() {
		// Close on an already-closed handle is a no-op, so this stays correct
		// whether or not the Reset below leaves the handle open.
		if err := ldb.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	original := localDBSchema
	localDBSchema = `create table ok (x integer) strict; create table nope(`
	t.Cleanup(func() { localDBSchema = original })

	if err := ldb.Reset(); err == nil {
		t.Fatal("Reset succeeded against a schema with a broken statement")
	}

	err = ldb.DB().Ping()
	if err == nil {
		t.Fatal("DB() is still usable after a failed Reset; a closed handle was expected")
	}
	if !errors.Is(err, sql.ErrConnDone) && !strings.Contains(err.Error(), "closed") {
		t.Fatalf("error from a handle left by a failed Reset = %v, want one that names the handle as closed", err)
	}
}
