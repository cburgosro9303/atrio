package store

import (
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	// Pure-Go SQLite driver, fixed by ADR-007. CGo is forbidden project-wide
	// because it breaks the cross-compile matrix; this driver is what makes
	// SQLite reachable without it.
	_ "modernc.org/sqlite"
)

//go:embed localdb.sql
var localDBSchema string

const (
	// localDBDriver is the name modernc.org/sqlite registers itself under.
	localDBDriver = "sqlite"

	// localDBFile is the database's name inside atrioDir. It is covered by
	// the .gitignore init generates, together with the -wal and -shm sidecars
	// WAL mode creates next to it.
	localDBFile = "atrio.db"

	// localDBGeneration is the DDL generation this build writes, stored in
	// the database's own `pragma user_version`.
	//
	// This is deliberately NOT called a schemaVersion: that name belongs to
	// the versioned JSON artifacts, where it drives a compatibility window and
	// real migration. This is a different concept with different rules —
	// finding a generation this build did not write means the file is
	// discarded and rebuilt, never migrated. ADR-006 is what licenses that:
	// everything in this database is derivable from the repository or
	// ephemeral by design, so there is nothing here whose loss is not either
	// recoverable or intended.
	localDBGeneration = 1
)

// LocalDB is Atrio's local SQLite database: the third store of ADR-006. It
// lives at .atrio/atrio.db, never travels in the repository, and holds only
// state that is derivable from the repository or ephemeral by design.
//
// What refills each table, and which task owns that:
//
//	document, document_tag,     derivable — reindexed from docs/**/*.md   T-022
//	document_relation,
//	document_issue, document_fts
//	materialization             derivable — definitions recompiled+hashed T-052
//	notification                ephemeral — born empty, nothing refills it T-042
//	session                     ephemeral — born empty                    T-070/T-080
//	project_lock                ephemeral — born empty                    T-031
//
// A table absent from that list would be a table nobody can account for after
// a rebuild, which ADR-006 calls out as the sign of data sitting in the wrong
// store. The ephemeral rows are lost on a generation change; that is the
// intended behaviour, not an accepted cost — an unread notification is not
// state the repository ever knew about.
//
// This type deliberately exposes no per-table accessors. The tasks that
// consume each table (T-022, T-042, T-052, T-031) are the ones that know the
// shape they need; inventing that shape here, with no caller to answer to,
// is the mistake T-020 avoided by keeping Document a generic map. DB gives
// them the handle to work against.
//
// Concurrency: the returned *sql.DB is safe for concurrent use, and the
// connection settings below give every connection the same pragmas. Reset is
// the exception — it closes and replaces the handle, so it must not run
// concurrently with anything holding the result of DB.
type LocalDB struct {
	db   *sql.DB
	path string
}

// OpenLocalDB opens (creating or rebuilding as needed) the local database of
// the project rooted at root, the same directory Open takes.
//
// A file whose generation this build did not write is discarded and rebuilt
// rather than migrated — see localDBGeneration for why that is sound.
func OpenLocalDB(root string) (*LocalDB, error) {
	// Resolve to an absolute path before anything uses it. A relative root
	// otherwise gets resolved twice against different bases: os.MkdirAll
	// resolves it against the process's working directory, while the SQLite
	// URI built from it is rooted at the filesystem root — so OpenLocalDB(".")
	// created .atrio/ where the caller meant and then asked the driver to open
	// /.atrio/atrio.db, which either fails outright or, running as root,
	// silently writes a database somewhere Path never reports. Resolving once,
	// here, is what keeps the directory, the DSN and Path talking about the
	// same file.
	path, err := filepath.Abs(filepath.Join(root, atrioDir, localDBFile))
	if err != nil {
		return nil, fmt.Errorf("store: resolving the local database path under %s: %w", root, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("store: creating %s: %w", filepath.Dir(path), err)
	}

	db, err := connectLocalDB(path)
	if err != nil {
		return nil, err
	}

	generation, err := readGeneration(db)
	if err != nil {
		return nil, errors.Join(err, db.Close())
	}
	if generation == localDBGeneration {
		return &LocalDB{db: db, path: path}, nil
	}

	rebuilt, err := discardAndBootstrap(db, path)
	if err != nil {
		return nil, err
	}
	return &LocalDB{db: rebuilt, path: path}, nil
}

// DB returns the handle consumers query through. See the type comment for why
// this package hands out the handle instead of per-table accessors.
func (l *LocalDB) DB() *sql.DB { return l.db }

// Path returns the database file's location on disk.
func (l *LocalDB) Path() string { return l.path }

// Close releases the database handle.
func (l *LocalDB) Close() error {
	if err := l.db.Close(); err != nil {
		return fmt.Errorf("store: closing %s: %w", l.path, err)
	}
	return nil
}

// Reset discards the database and rebuilds it empty: the half of "regenerate
// completely from the repository" that this package owns. Refilling the
// derivable tables afterwards belongs to the tasks that fill them in the
// first place — reindexing docs/**/*.md is T-022, rehashing materialized
// files is T-052 — because reconstruction is the same code path as the
// initial construction, not a second implementation of it.
//
// The handle returned by DB before this call is closed by it; see the type
// comment on concurrency.
//
// On failure this LocalDB is left unusable, and deliberately so: discarding
// closes the handle before removing the file, so by the time anything can go
// wrong there is no earlier state to roll back to — the database on disk is
// already gone. What the caller keeps is a closed handle, which answers every
// subsequent query with "sql: database is closed" rather than appearing to
// work; recovering means calling Close and opening again. That contract is
// asserted by TestReset_LeavesAClosedHandleWhenItFails, because "fails loudly"
// is only worth writing down if something checks it stays true.
func (l *LocalDB) Reset() error {
	rebuilt, err := discardAndBootstrap(l.db, l.path)
	if err != nil {
		return err
	}
	l.db = rebuilt
	return nil
}

// connectLocalDB opens the file at path with this package's connection
// settings applied. It does not look at, or create, any schema.
func connectLocalDB(path string) (*sql.DB, error) {
	db, err := sql.Open(localDBDriver, localDBDSN(path))
	if err != nil {
		return nil, fmt.Errorf("store: opening %s: %w", path, err)
	}

	// Reach the file once here so a bad path, a directory where a database
	// should be, or a corrupt header surfaces as an error from this call.
	// sql.Open alone connects to nothing.
	if err := db.Ping(); err != nil {
		return nil, errors.Join(fmt.Errorf("store: opening %s: %w", path, err), db.Close())
	}

	if err := tightenLocalDBPermissions(path); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return db, nil
}

// tightenLocalDBPermissions narrows the database and its sidecars to owner-only.
//
// The driver creates them at the process umask's default — 0644 on a typical
// machine, verified — while this package's other writer hands 0o600 to
// os.OpenFile at creation time (writeNew, store/fsio.go). There is no
// equivalent knob here, since SQLite is what creates the files, so the mode is
// narrowed immediately after connecting instead.
//
// Only atrio.db normally exists at this point: the sidecars are not created by
// connecting with WAL in the DSN, but by the first write transaction, which
// has not run yet — verified. That is fine, and not a hole, because SQLite
// creates them matching the mode of the database file beside them, so
// narrowing atrio.db first is what makes the sidecars land at 0600 too — also
// verified. Iterating all three anyway is what catches the ones this process
// did not create: a sidecar a crashed run left behind, or one a foreign tool
// made at 0644. Their absence is therefore the ordinary case, which is why
// ErrNotExist is skipped rather than reported.
//
// A failure is returned rather than ignored: the alternative is carrying on
// with a readable database while claiming otherwise. Filesystems without
// permission bits (FAT32, some network shares) succeed here as no-ops, the
// same way 0o600 is a no-op for them in writeNew.
func tightenLocalDBPermissions(path string) error {
	for _, name := range localDBFiles(path) {
		err := os.Chmod(name, 0o600)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("store: restricting permissions on %s: %w", name, err)
		}
	}
	return nil
}

// localDBDSN builds the driver's connection string for path, as a SQLite URI
// filename carrying the pragmas as query parameters. path must already be
// absolute — OpenLocalDB is the one place that resolves it, and the leading
// slash added below is for a Windows drive letter, not a way to absolutize a
// relative path.
//
// The URI form is not decoration, and the mechanism is worth naming because
// the alternative looks equivalent. The driver splits a DSN at its first '?'
// — but only truncates the path there when the DSN does *not* begin with
// "file:"; a URI is handed to sqlite3_open_v2 whole, with SQLITE_OPEN_URI, and
// SQLite's own URI parser separates path from query and undoes the escaping.
// So with a raw path, a project directory whose name contains a '?' makes
// everything after it read as query parameters: the database is silently
// created somewhere else *and* the pragmas are silently dropped — including
// foreign_keys, so the ON DELETE CASCADEs in localdb.sql stop firing and
// orphan rows accumulate in a database nobody is looking at. Verified against
// a directory named "with?question"; covered by
// TestLocalDBDSN_AppliesPragmasOnAwkwardPaths. It is also what makes the
// Windows form work, since "file:///C:/..." is a shape SQLite parses and a
// bare drive-lettered path is not.
//
// Known limit, documented rather than guessed at: a Windows UNC root
// (\\server\share\...) becomes "file:////server/share/..." here, and whether
// SQLite resolves that back to the share has not been verified — CI's Windows
// runner only ever exercises a local drive. SQLite's own documentation
// discourages database files on network shares anyway, since the locking it
// relies on is not dependable there, so the gap is noted rather than papered
// over with untested handling.
//
// Both pragmas must be attached per connection, not executed once after
// opening: PRAGMA state is per connection, so a `db.Exec("pragma
// foreign_keys=on")` applies only to whichever pooled connection happened to
// serve it, and every other connection in the pool keeps the defaults.
// journal_mode is the exception — it is a property of the file rather than of
// a connection — but it is listed here too so that one place lists them all.
func localDBDSN(path string) string {
	slashed := filepath.ToSlash(path)
	// A SQLite URI filename is absolute after "file:"; on Windows an absolute
	// path starts with a drive letter, which needs the leading slash added.
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}

	pragmas := url.Values{}
	pragmas["_pragma"] = []string{
		// Enforce the ON DELETE CASCADEs in localdb.sql. Off by default in
		// SQLite, and off means the cascades silently do nothing.
		"foreign_keys(1)",
		// Wait rather than fail on a locked database: several Atrio processes
		// (CLI, portal, agent runs) can reach one project.
		"busy_timeout(5000)",
		// Concurrent readers alongside a writer. Persistent in the file, so
		// this is idempotent after the first connection.
		"journal_mode(wal)",
	}

	// Encode percent-escapes the parentheses in the pragma values, so the DSN
	// carries foreign_keys%281%29 rather than foreign_keys(1). That is correct
	// and verified — the driver decodes the query before reading it, and all
	// three pragmas land — but it looks wrong enough to invite a "fix" back to
	// a hand-built query string, which is what loses the escaping of the path.
	uri := url.URL{Scheme: "file", Path: slashed, RawQuery: pragmas.Encode()}
	return uri.String()
}

// readGeneration reports the DDL generation stored in the file, which is 0 for
// a database this package has never bootstrapped (SQLite's default).
func readGeneration(db *sql.DB) (int, error) {
	var generation int
	if err := db.QueryRow(`pragma user_version`).Scan(&generation); err != nil {
		return 0, fmt.Errorf("store: reading the local database generation: %w", err)
	}
	return generation, nil
}

// discardAndBootstrap closes db, removes the database file together with the
// sidecars WAL mode leaves next to it, and returns a freshly bootstrapped
// handle on the same path.
//
// Removing the file is what makes this total: dropping the tables one by one
// would have to keep pace with localdb.sql forever, and would leave behind
// the shadow tables FTS5 creates under names this package never wrote.
func discardAndBootstrap(db *sql.DB, path string) (*sql.DB, error) {
	if err := db.Close(); err != nil {
		return nil, fmt.Errorf("store: closing %s before rebuilding it: %w", path, err)
	}
	for _, name := range localDBFiles(path) {
		if err := os.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("store: removing %s: %w", name, err)
		}
	}

	fresh, err := connectLocalDB(path)
	if err != nil {
		return nil, err
	}
	if err := bootstrapLocalDB(fresh); err != nil {
		return nil, errors.Join(err, fresh.Close())
	}
	return fresh, nil
}

// localDBFiles lists every file the database occupies: the database itself
// plus the write-ahead log and shared-memory sidecars WAL mode creates. A
// clean close removes the sidecars on its own; a process that died holding
// the database does not, which is exactly when this list matters.
func localDBFiles(path string) []string {
	return []string{path, path + "-wal", path + "-shm"}
}

// bootstrapLocalDB creates the schema and stamps the generation, both inside
// one transaction.
//
// The transaction is the crash-safety guarantee, not a formality: SQLite
// applies DDL and `pragma user_version` transactionally together (verified —
// rolling this back leaves zero tables and generation 0), so a process killed
// mid-bootstrap can never leave a half-built database stamped as current. That
// is what lets OpenLocalDB trust the generation alone and skip inspecting the
// tables.
func bootstrapLocalDB(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("store: starting the local database bootstrap: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after a successful commit is a no-op error this path cannot act on; the commit below is what is checked

	if _, err := tx.Exec(localDBSchema); err != nil {
		return fmt.Errorf("store: creating the local database schema: %w", err)
	}
	// Not a placeholder: SQLite does not bind parameters in a PRAGMA. The
	// value is this package's own untyped constant, never a caller's input.
	if _, err := tx.Exec(fmt.Sprintf(`pragma user_version = %d`, localDBGeneration)); err != nil {
		return fmt.Errorf("store: stamping the local database generation: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: committing the local database bootstrap: %w", err)
	}
	return nil
}
