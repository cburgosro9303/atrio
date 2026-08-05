package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	// atrioDir is the project-local folder every artifact this package
	// manages lives under.
	atrioDir = ".atrio"
	// managementDir holds one subfolder per per-ulid artifact kind.
	managementDir = ".atrio/management"

	// currentSchemaVersion is the schema generation this build writes.
	// Every schema in schemas/ is at its first generation (T-010/T-011/T-012);
	// reading N-1 and N-2 and migrating forward is real work with no
	// generation-2 schema yet to migrate towards, so it is left to whichever
	// task first needs it rather than built speculatively here.
	currentSchemaVersion = 1
)

// artifactKind describes one family of per-ULID artifact this repository
// manages: which schema validates it, which folder under managementDir holds
// its files, whether it may ever be overwritten once created, and how to
// find the references inside it that this package can check by file
// existence (nil when a kind carries none).
type artifactKind struct {
	schema     string
	dir        string
	appendOnly bool
	refs       func(Document) []reference
}

var (
	taskKind = artifactKind{schema: "task.schema.json", dir: "tasks"}

	decisionKind = artifactKind{
		schema: "decision.schema.json",
		dir:    "decisions",
		refs: func(d Document) []reference {
			return referencesFromArray(d["refs"], "refs")
		},
	}

	logEntryKind = artifactKind{
		schema:     "log-entry.schema.json",
		dir:        "log",
		appendOnly: true,
		refs: func(d Document) []reference {
			return referencesFromArray(d["refs"], "refs")
		},
	}

	changelogKind = artifactKind{
		schema: "changelog.schema.json",
		dir:    "changelogs",
		refs: func(d Document) []reference {
			return referencesFromArray(d["impacts"], "impacts")
		},
	}

	flowProgressKind = artifactKind{
		schema: "flow-progress.schema.json",
		dir:    "flows",
		refs: func(d Document) []reference {
			stages, ok := d["stages"].([]any)
			if !ok {
				return nil
			}
			var refs []reference
			for i, s := range stages {
				stage, ok := asMap(s)
				if !ok {
					continue
				}
				field := fmt.Sprintf("stages/%d/outputRefs", i)
				refs = append(refs, referencesFromArray(stage["outputRefs"], field)...)
			}
			return refs
		},
	}
)

// Repository is Atrio's persistence layer over versioned JSON artifacts: one
// file per entity, validated against its JSON Schema on every read and
// write, attributed to the git identity injected into it.
//
// A Repository's own state (its compiled schemas, its id generator) is safe
// to use from concurrent goroutines. What it does not provide is
// serialization between two writes to the *same* artifact: createArtifact
// picks a fresh id per call, so two concurrent Create calls cannot collide,
// but updateArtifact and writeSingleton are read-modify-write — two
// concurrent updates to the same task, or two concurrent WriteProjectConfig
// calls, can both read the same version and race on which one lands last,
// last-write-wins, with no lock in between. Serializing writes across
// processes is T-021's job (it owns locks, by name, in the backlog); this
// package does not invent that mechanism ahead of it.
type Repository struct {
	root     string
	identity Identity
	schemas  map[string]*jsonschema.Schema
	newID    func() (string, error)
}

// Open returns a Repository rooted at root, the directory that contains (or
// will contain) .atrio. identity supplies the git user this repository
// attributes its writes to (T-030 wires in the real implementation; store
// only depends on the Identity interface).
func Open(root string, identity Identity) (*Repository, error) {
	if identity == nil {
		return nil, errors.New("store: identity must not be nil")
	}

	compiled, err := compileSchemas()
	if err != nil {
		return nil, err
	}

	return &Repository{
		root:     root,
		identity: identity,
		schemas:  compiled,
		newID:    newIDGenerator().next,
	}, nil
}

func (r *Repository) artifactPath(kind artifactKind, id string) string {
	return filepath.Join(r.root, managementDir, kind.dir, id+".json")
}

// stampCreate returns business with the envelope of a brand-new artifact
// attached: a fresh id, both timestamps set to now, and createdBy set to
// createdBy. business is not mutated.
func stampCreate(id string, now time.Time, createdBy Document, business Document) Document {
	doc := business.clone()
	doc["schemaVersion"] = currentSchemaVersion
	doc["id"] = id
	doc["createdAt"] = formatTimestamp(now)
	doc["updatedAt"] = formatTimestamp(now)
	doc["createdBy"] = createdBy
	return doc
}

// stampUpdate returns business with the envelope of an update to existing
// attached: identity, schema generation and createdBy carry over unchanged —
// none of those can be decided by the caller of Update — and updatedAt moves
// to now. business is not mutated.
//
// Preserving schemaVersion here is a deliberate, named divergence from
// schemas/README.md's own convention ("la versión N ... siempre escribe
// N"): re-stamping a document that was not actually migrated as if it were
// the current generation would misrepresent it, and this package does not
// implement migration. It is invisible today because every schema is still
// at generation 1 — see T-020's closure entry in docs/spec/04-backlog-m1.md
// for the trigger that forces reconciling it (a second schema generation,
// or real migration logic landing).
func stampUpdate(existing Document, now time.Time, business Document) Document {
	doc := business.clone()
	doc["schemaVersion"] = existing["schemaVersion"]
	doc["id"] = existing["id"]
	doc["createdAt"] = existing["createdAt"]
	doc["updatedAt"] = formatTimestamp(now)
	doc["createdBy"] = existing["createdBy"]
	return doc
}

// formatTimestamp renders t as the RFC 3339 form common.schema.json's
// timestamp definition requires. Nanosecond precision (not the bare-seconds
// RFC3339 form) is what keeps two envelope timestamps stamped microseconds
// apart from colliding into the same string — createdAt and updatedAt on a
// freshly created artifact, or updatedAt across two updates issued in quick
// succession.
func formatTimestamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// createArtifact assigns a new id, stamps the envelope, validates and
// writes a brand-new artifact of the given kind. business must hold only
// the artifact's business fields: any envelope field it happens to carry is
// discarded, since the envelope is this method's responsibility, not the
// caller's.
func (r *Repository) createArtifact(kind artifactKind, business Document) (string, Document, error) {
	id, err := r.newID()
	if err != nil {
		return "", nil, err
	}
	createdBy, err := r.actor()
	if err != nil {
		return "", nil, err
	}

	doc := stampCreate(id, time.Now(), createdBy, business)
	path := r.artifactPath(kind, id)

	if err := r.validate(kind.schema, path, doc); err != nil {
		return "", nil, err
	}
	if kind.refs != nil {
		if fields := r.checkReferences(kind.refs(doc)); len(fields) > 0 {
			return "", nil, &ArtifactValidationError{Path: path, Fields: fields}
		}
	}

	data, err := encode(doc)
	if err != nil {
		return "", nil, err
	}

	write := writeReplacing
	if kind.appendOnly {
		write = writeNew
	}
	if err := write(path, data); err != nil {
		return "", nil, err
	}

	// Re-read what was just written rather than returning doc as built in
	// memory: doc holds Go values this package's own callers can pass in
	// (Document, int, []any of either), while readDocument decodes strictly
	// through jsonschema.UnmarshalJSON (plain map[string]any, json.Number).
	// Returning the in-memory doc here would mean Create and Read hand back
	// differently-typed documents for the same field — a caller's type
	// assertion that works against a freshly created document could fail
	// against the very same document read back later. This costs one extra
	// read per write to guarantee they never diverge.
	stored, err := readDocument(path)
	if err != nil {
		return "", nil, fmt.Errorf("re-reading %s after writing it: %w", path, err)
	}
	return id, stored, nil
}

// readArtifact loads and validates one artifact of the given kind.
func (r *Repository) readArtifact(kind artifactKind, id string) (Document, error) {
	if !isWellFormedULID(id) {
		return nil, fmt.Errorf("store: %q is not a well-formed ulid", id)
	}

	path := r.artifactPath(kind, id)
	doc, err := readDocument(path)
	if err != nil {
		return nil, err
	}
	if err := r.validate(kind.schema, path, doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// updateArtifact replaces the business fields of an existing artifact,
// preserving its id, createdAt and createdBy and moving updatedAt to now.
// It is not available for an append-only kind (log-entry.go does not expose
// an Update at all: nothing calls this with logEntryKind).
func (r *Repository) updateArtifact(kind artifactKind, id string, business Document) (Document, error) {
	if !isWellFormedULID(id) {
		return nil, fmt.Errorf("store: %q is not a well-formed ulid", id)
	}

	path := r.artifactPath(kind, id)
	existing, err := readDocument(path)
	if err != nil {
		return nil, err
	}

	doc := stampUpdate(existing, time.Now(), business)
	if err := r.validate(kind.schema, path, doc); err != nil {
		return nil, err
	}
	if kind.refs != nil {
		if fields := r.checkReferences(kind.refs(doc)); len(fields) > 0 {
			return nil, &ArtifactValidationError{Path: path, Fields: fields}
		}
	}

	data, err := encode(doc)
	if err != nil {
		return nil, err
	}
	if err := writeReplacing(path, data); err != nil {
		return nil, err
	}

	// See the matching comment in createArtifact: returning what was just
	// written, decoded the same way a later Read decodes it, is what keeps
	// Create/Update and Read agreeing on the Go types of the same fields.
	stored, err := readDocument(path)
	if err != nil {
		return nil, fmt.Errorf("re-reading %s after writing it: %w", path, err)
	}
	return stored, nil
}

// listArtifactIDs lists every id stored for the given kind. The result is
// sorted, which for a ULID is also chronological order; a kind whose folder
// does not exist yet reports no error and an empty list.
func (r *Repository) listArtifactIDs(kind artifactKind) ([]string, error) {
	dir := filepath.Join(r.root, managementDir, kind.dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing %s: %w", dir, err)
	}

	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if filepath.Ext(name) != ".json" {
			continue
		}
		ids = append(ids, strings.TrimSuffix(name, ".json"))
	}
	sort.Strings(ids)
	return ids, nil
}

// singletonCheck is a code-level rule a singleton document (project config,
// the agent roster) must satisfy beyond its schema: comparing the version
// about to be written against the one it replaces, or checking a rule
// between siblings inside the one document a schema cannot check against
// itself. existing is nil the first time the singleton is written. A nil
// result means the rule is satisfied.
type singletonCheck func(existing, incoming Document) []FieldError

// writeSingleton creates or replaces the one artifact at path: the first
// write assigns id/createdAt/createdBy, and every write after that preserves
// them and moves updatedAt to now — the same envelope rules createArtifact
// and updateArtifact apply to a per-ulid artifact, applied here to a
// fixed-path one instead.
func (r *Repository) writeSingleton(path, schemaName string, business Document, check singletonCheck) (Document, error) {
	existing, err := readDocument(path)
	switch {
	case err == nil:
		// fall through with existing populated: this is an update.
	case isNotFound(err):
		existing = nil
	default:
		return nil, err
	}

	now := time.Now()
	var doc Document
	if existing == nil {
		id, err := r.newID()
		if err != nil {
			return nil, err
		}
		createdBy, err := r.actor()
		if err != nil {
			return nil, err
		}
		doc = stampCreate(id, now, createdBy, business)
	} else {
		doc = stampUpdate(existing, now, business)
	}

	if err := r.validate(schemaName, path, doc); err != nil {
		return nil, err
	}
	if check != nil {
		if fields := check(existing, doc); len(fields) > 0 {
			return nil, &ArtifactValidationError{Path: path, Fields: fields}
		}
	}

	data, err := encode(doc)
	if err != nil {
		return nil, err
	}
	if err := writeReplacing(path, data); err != nil {
		return nil, err
	}

	// See the matching comment in createArtifact: returning what was just
	// written, decoded the same way a later Read decodes it, is what keeps
	// a write and a read agreeing on the Go types of the same fields.
	stored, err := readDocument(path)
	if err != nil {
		return nil, fmt.Errorf("re-reading %s after writing it: %w", path, err)
	}
	return stored, nil
}

// readSingleton loads and validates the one artifact at path.
func (r *Repository) readSingleton(path, schemaName string) (Document, error) {
	doc, err := readDocument(path)
	if err != nil {
		return nil, err
	}
	if err := r.validate(schemaName, path, doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func isNotFound(err error) bool {
	var notFound *NotFoundError
	return errors.As(err, &notFound)
}
