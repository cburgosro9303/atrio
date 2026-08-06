package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// NotificationLevel is a notification's severity, ordered LevelInfo <
// LevelWarning < LevelError. The order is not decorative: List's MinLevel
// filter needs it to answer "warning and above" without hard-coding which
// levels that means.
//
// This catalog is closed in Go, not in a `CHECK` on notification.level in
// localdb.sql. That is deliberate, not an oversight: this file is the only
// writer of that table, so a `CHECK` would be a second guardian of a rule
// this package already enforces, at the cost of a full-database discard
// (bumping localDBGeneration) every time the catalog grows. localdb.sql's own
// comment on the notification table draws the same line the schemas package
// draws for requiredCapabilities: membership is verified in code so that
// adding a value is a platform release, not a breaking change to a published
// contract. Contrast with document_relation.kind, which DOES carry a `CHECK`
// in the DDL — that catalog is already frozen in a public JSON Schema, and
// notification's is not frozen anywhere.
type NotificationLevel string

const (
	LevelInfo    NotificationLevel = "info"
	LevelWarning NotificationLevel = "warning"
	LevelError   NotificationLevel = "error"
)

// Valid reports whether l is one of the three levels this package writes.
func (l NotificationLevel) Valid() bool {
	switch l {
	case LevelInfo, LevelWarning, LevelError:
		return true
	default:
		return false
	}
}

// severity orders l for the MinLevel comparison in NotificationFilter. Not
// exported: nothing outside this file needs the numeric encoding, only the
// ordering it produces.
func (l NotificationLevel) severity() int {
	switch l {
	case LevelInfo:
		return 0
	case LevelWarning:
		return 1
	case LevelError:
		return 2
	default:
		return -1
	}
}

// NotificationCategory is what produced a notification. Closed in Go for the
// same reason NotificationLevel is — see its comment.
//
// Each constant is not invented: it is one of the events the spec already
// names as a notification producer, cited here so the mapping from event to
// category is traceable back to its source and to the task that will emit
// it.
type NotificationCategory string

const (
	// CategoryDocument: an invalid document is marked rather than dropped,
	// and a manual edit that breaks structure is attributed and reported as
	// an "impact notification" (03-arquitectura.md:85,
	// 01-definicion-producto.md:120 and :270). Emitted by T-022.
	CategoryDocument NotificationCategory = "document"
	// CategoryFlow: a stage whose closing extraction fails persistently moves
	// to pendiente_de_cierre and notifies (03-arquitectura.md:94,
	// 01-definicion-producto.md:197). Emitted by T-071.
	CategoryFlow NotificationCategory = "flow"
	// CategoryCatalog: a published changelog of catalog definitions reaches
	// the team's notification panel (01-definicion-producto.md:94). Emitted
	// by T-061.
	CategoryCatalog NotificationCategory = "catalog"
	// CategoryMigration: a prior notification before an artifact format
	// change, and the upgrade's own changelog to the notification panel
	// (01-definicion-producto.md:97, 03-arquitectura.md:115 and :117).
	// Emitted by T-081.
	CategoryMigration NotificationCategory = "migration"
)

// Valid reports whether c is one of the four categories this package writes.
func (c NotificationCategory) Valid() bool {
	switch c {
	case CategoryDocument, CategoryFlow, CategoryCatalog, CategoryMigration:
		return true
	default:
		return false
	}
}

// Notification is one row of the notification table (localdb.sql), the
// ephemeral store T-021 left empty for this task to fill: nothing refills it
// on rebuild, and losing an unread notification on a generation change is the
// intended behaviour, not an accepted cost.
type Notification struct {
	ID        string
	CreatedAt time.Time
	Level     NotificationLevel
	Category  NotificationCategory
	Title     string
	Body      string
	// ReadAt is nil while the notification has not been read.
	ReadAt *time.Time
}

// NotificationFilter narrows List's result. The zero value matches every
// notification: MinLevel empty means no level filter (not "info and above" —
// see the comment inside List for why LevelInfo and "" are deliberately
// different queries), an empty Categories means every category, and Limit
// zero means no limit.
type NotificationFilter struct {
	// MinLevel, if non-empty, keeps only notifications at this level or more
	// severe.
	MinLevel NotificationLevel
	// Categories, if non-empty, keeps only notifications in one of these
	// categories.
	Categories []NotificationCategory
	// UnreadOnly keeps only notifications with a nil ReadAt.
	UnreadOnly bool
	// Limit caps the number of rows returned. Zero means no limit; negative
	// is a validation error.
	Limit int
}

// NotificationValidationError is returned when a notification or a filter is
// rejected for a reason the caller can fix: an out-of-catalog level or
// category, an empty title, or a negative Limit. It is a distinct type from
// ArtifactValidationError on purpose — a notification is not a JSON artifact
// under management/, and reusing that type's name would misrepresent what
// was rejected.
type NotificationValidationError struct {
	Fields []FieldError
}

func (e *NotificationValidationError) Error() string {
	parts := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		parts[i] = f.String()
	}
	return fmt.Sprintf("notification rejected: %s", strings.Join(parts, "; "))
}

// Notifications is Atrio's internal notification store, backed by the
// notification table T-021 created and left empty. It has no accessor
// counterpart in LocalDB by design (store/localdb.go's type comment): the
// shape of this API is this task's to decide, not LocalDB's.
//
// No context.Context anywhere in this type: the rest of store/ (Repository,
// LocalDB) is free of it, and localdb.go itself calls Exec/QueryRow without
// the *Context variants. Adding ctx only here would leave the package
// internally inconsistent for no caller that exists yet. If api/ eventually
// needs cancellation, the package adopts context as a whole, not through one
// type reaching ahead of the rest.
type Notifications struct {
	db    *LocalDB
	newID func() (string, error)
	now   func() time.Time
}

// NewNotifications returns a Notifications backed by db. It never caches
// db.DB(): every method below calls it fresh, so a Reset() between two calls
// is observed instead of leaving the second call working against a closed
// handle. Serializing concurrent writers across processes is still T-031's
// job, as it was left for T-021 — this type does not invent that mechanism
// ahead of it.
func NewNotifications(db *LocalDB) *Notifications {
	return &Notifications{
		db:    db,
		newID: newIDGenerator().next,
		now:   time.Now,
	}
}

// Notify records a new notification and returns exactly what a List call
// right after would return for it: it inserts, then re-reads the row it just
// wrote, rather than trusting the values handed to it. That mirrors T-020's
// Create/Update/Write, which re-read what they just wrote for the same
// reason (04-backlog-m1.md:120).
func (n *Notifications) Notify(level NotificationLevel, category NotificationCategory, title, body string) (Notification, error) {
	var fields []FieldError
	if !level.Valid() {
		fields = append(fields, FieldError{Field: "level", Reason: fmt.Sprintf("%q is not a recognized notification level", string(level))})
	}
	if !category.Valid() {
		fields = append(fields, FieldError{Field: "category", Reason: fmt.Sprintf("%q is not a recognized notification category", string(category))})
	}
	if title == "" {
		fields = append(fields, FieldError{Field: "title", Reason: "must not be empty"})
	}
	if len(fields) > 0 {
		return Notification{}, &NotificationValidationError{Fields: fields}
	}

	id, err := n.newID()
	if err != nil {
		return Notification{}, err
	}
	createdAt := formatTimestamp(n.now())

	db := n.db.DB()
	if _, err := db.Exec(
		`insert into notification (id, created_at, level, category, title, body) values (?, ?, ?, ?, ?, ?)`,
		id, createdAt, string(level), string(category), title, body,
	); err != nil {
		return Notification{}, fmt.Errorf("store: inserting notification %s: %w", id, err)
	}

	notif, err := scanNotification(db.QueryRow(
		`select id, created_at, level, category, title, body, read_at from notification where id = ?`,
		id,
	))
	if err != nil {
		return Notification{}, fmt.Errorf("store: re-reading notification %s after writing it: %w", id, err)
	}
	return notif, nil
}

// List returns the notifications matching filter, newest first. Ties in
// created_at break on id descending — a ULID sorts chronologically, so this
// is what makes the order total rather than merely "usually" stable when two
// notifications are minted within the same stored timestamp.
func (n *Notifications) List(filter NotificationFilter) (results []Notification, err error) {
	if verr := filter.validate(); verr != nil {
		return nil, verr
	}

	var (
		conditions []string
		args       []any
	)

	// MinLevel and "" are deliberately different queries: "" adds no level
	// predicate at all (every level matches, including any value on disk
	// outside today's catalog), while LevelInfo explicitly restricts to
	// {info, warning, error}. Do not fold the empty case into LevelInfo.
	if filter.MinLevel != "" {
		var atLeast []string
		for _, l := range []NotificationLevel{LevelInfo, LevelWarning, LevelError} {
			if l.severity() >= filter.MinLevel.severity() {
				atLeast = append(atLeast, string(l))
			}
		}
		conditions = append(conditions, "level in ("+placeholders(len(atLeast))+")")
		for _, l := range atLeast {
			args = append(args, l)
		}
	}

	if len(filter.Categories) > 0 {
		conditions = append(conditions, "category in ("+placeholders(len(filter.Categories))+")")
		for _, c := range filter.Categories {
			args = append(args, string(c))
		}
	}

	if filter.UnreadOnly {
		conditions = append(conditions, "read_at is null")
	}

	query := strings.Builder{}
	query.WriteString(`select id, created_at, level, category, title, body, read_at from notification`)
	if len(conditions) > 0 {
		query.WriteString(" where ")
		query.WriteString(strings.Join(conditions, " and "))
	}
	query.WriteString(" order by created_at desc, id desc")
	if filter.Limit > 0 {
		query.WriteString(" limit ?")
		args = append(args, filter.Limit)
	}

	rows, queryErr := n.db.DB().Query(query.String(), args...)
	if queryErr != nil {
		return nil, fmt.Errorf("store: listing notifications: %w", queryErr)
	}
	// Named return + defer, rather than a suppressed rows.Close(), so a close
	// failure is not silently dropped (errcheck's check-blank forbids that
	// project-wide). A scan failure or rows.Err always wins — err is already
	// non-nil by the time this closure runs, so the close error is only
	// surfaced when nothing else already explains why List failed. Same
	// intent as the errors.Join(err, x.Close()) pattern localdb.go uses at
	// its own explicit return points, adapted to a loop with more than one
	// exit.
	defer func() {
		if cerr := rows.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("store: closing notification rows: %w", cerr)
		}
	}()

	for rows.Next() {
		notif, scanErr := scanNotification(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		results = append(results, notif)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("store: iterating notification rows: %w", rowsErr)
	}
	return results, nil
}

// validate checks the fields of f that this package can judge without
// touching the database: MinLevel is empty or a known level, every category
// is known, and Limit is not negative.
func (f NotificationFilter) validate() error {
	var fields []FieldError
	if f.MinLevel != "" && !f.MinLevel.Valid() {
		fields = append(fields, FieldError{Field: "minLevel", Reason: fmt.Sprintf("%q is not a recognized notification level", string(f.MinLevel))})
	}
	for i, c := range f.Categories {
		if !c.Valid() {
			fields = append(fields, FieldError{Field: fmt.Sprintf("categories/%d", i), Reason: fmt.Sprintf("%q is not a recognized notification category", string(c))})
		}
	}
	if f.Limit < 0 {
		fields = append(fields, FieldError{Field: "limit", Reason: "must not be negative"})
	}
	if len(fields) > 0 {
		return &NotificationValidationError{Fields: fields}
	}
	return nil
}

// MarkRead stamps read_at on every notification named by ids, using the
// injected clock. It is:
//
//   - Validated up front: every id must be a well-formed ULID before this
//     method touches the database at all — the same defense readArtifact and
//     updateArtifact already apply to a caller-supplied id, extended here to
//     a whole batch before any of them reaches SQL.
//   - Transactional: ids are applied one by one inside a single transaction,
//     so one nonexistent id in a batch leaves none of the others marked —
//     the transaction is rolled back instead of partially committed.
//   - Idempotent: the update only touches rows where read_at is still null,
//     so re-marking an already-read notification does not move its read_at
//     forward.
//   - A no-op on zero ids: nothing is validated, no transaction opens, no
//     error is returned.
//
// The nonexistent-id check below runs as a read (QueryRow) issued only after
// the update (Exec) on the same id. That ordering used to be load-bearing:
// under the driver's default deferred BEGIN, this transaction's write lock
// was acquired by whichever statement first needed one, and promoting an
// already-open read into a write transaction can return SQLITE_BUSY
// immediately rather than honoring busy_timeout — so the write had to come
// first, or busy_timeout's own promise ("wait rather than fail on a locked
// database") would not actually hold for this method. That gap is what T-022
// measured directly against this package's schema (store/localdb.go's
// localDBDSN comment has the full account) and closed at its source:
// localDBDSN now opens every connection with _txlock=immediate, so
// db.Begin() itself acquires the write lock as its opening statement,
// before any statement below runs, regardless of which one it is. The
// ordering here is kept — it still reads naturally as "make the change,
// then check what happened" — but the DSN, not this ordering, is what
// stands between this transaction and SQLITE_BUSY now.
func (n *Notifications) MarkRead(ids ...string) error {
	if len(ids) == 0 {
		return nil
	}

	for _, id := range ids {
		if !isWellFormedULID(id) {
			return fmt.Errorf("store: %q is not a well-formed ulid", id)
		}
	}

	db := n.db.DB()
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("store: starting mark-read transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after a successful commit is a no-op error this path cannot act on; the commit below is what is checked

	readAt := formatTimestamp(n.now())
	var missing []string
	for _, id := range ids {
		res, err := tx.Exec(
			`update notification set read_at = ? where id = ? and read_at is null`,
			readAt, id,
		)
		if err != nil {
			return fmt.Errorf("store: marking notification %s read: %w", id, err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("store: marking notification %s read: %w", id, err)
		}
		if affected > 0 {
			continue
		}

		// No row moved: either id does not exist, or it is already read
		// (the idempotent case, not an error). Tell them apart.
		var exists int
		if err := tx.QueryRow(`select count(*) from notification where id = ?`, id).Scan(&exists); err != nil {
			return fmt.Errorf("store: checking notification %s: %w", id, err)
		}
		if exists == 0 {
			missing = append(missing, id)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("store: no such notification(s): %s", strings.Join(missing, ", "))
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: committing mark-read: %w", err)
	}
	return nil
}

// placeholders returns n comma-separated "?" placeholders for a SQL IN
// clause. Categories and levels reach SQL only through these placeholders,
// bound as parameters — never interpolated into the query text, the same
// discipline CLAUDE.md requires for external process arguments.
func placeholders(n int) string {
	marks := make([]string, n)
	for i := range marks {
		marks[i] = "?"
	}
	return strings.Join(marks, ", ")
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows, letting
// scanNotification serve Notify's single-row re-read and List's multi-row
// scan with one implementation.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanNotification decodes one row shaped like the select statements above:
// id, created_at, level, category, title, body, read_at, in that order.
//
// Reads are tolerant of a level or category that is not in today's Go
// catalog — the row is still returned, not rejected — because closing the
// catalog in code rather than in a DDL CHECK (see NotificationLevel's
// comment) means an older or foreign value already on disk must still be
// readable. What is NOT tolerated is a created_at or read_at that fails to
// parse as RFC 3339: that is not a foreign-but-valid value, it is corruption,
// and returning a zero time.Time in that case would hide it as if the field
// had simply never been read.
func scanNotification(row rowScanner) (Notification, error) {
	var (
		id, createdAtRaw, level, category, title, body string
		readAtRaw                                      sql.NullString
	)
	if err := row.Scan(&id, &createdAtRaw, &level, &category, &title, &body, &readAtRaw); err != nil {
		return Notification{}, fmt.Errorf("store: scanning notification: %w", err)
	}

	createdAt, err := time.Parse(time.RFC3339Nano, createdAtRaw)
	if err != nil {
		return Notification{}, fmt.Errorf("store: notification %s has a created_at that is not RFC 3339: %w", id, err)
	}

	var readAt *time.Time
	if readAtRaw.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, readAtRaw.String)
		if err != nil {
			return Notification{}, fmt.Errorf("store: notification %s has a read_at that is not RFC 3339: %w", id, err)
		}
		readAt = &parsed
	}

	return Notification{
		ID:        id,
		CreatedAt: createdAt,
		Level:     NotificationLevel(level),
		Category:  NotificationCategory(category),
		Title:     title,
		Body:      body,
		ReadAt:    readAt,
	}, nil
}
