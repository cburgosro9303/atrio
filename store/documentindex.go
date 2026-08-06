package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Attribution names who last edited a document, and in which commit. It is
// this package's own type rather than gitops.Attribution — store imports
// nothing else in the module (ADR-016), and gitops is a sibling package, not
// an ancestor. The seam is the same shape as Identity (identity.go): whoever
// wires a DocumentIndexer together (T-080) supplies an adapter over
// gitops.Binary.LastEditor, and this package never imports gitops to get
// there.
type Attribution struct {
	Commit string
	Name   string
	Email  string
}

// Attributor answers "who last edited path", the seam that keeps this
// package from importing gitops. A DocumentIndexer's attributor is never
// nil (NewDocumentIndexer refuses to build one without it) precisely so that
// degrading to an empty Attribution on failure — see Reindex — is a decision
// this package always makes deliberately, never one it falls into because
// nothing was wired up.
type Attributor interface {
	// LastEditor identifies who last changed path, and in which commit.
	LastEditor(path string) (Attribution, error)
}

// DocumentRelation is a typed link from an indexed document to another
// artifact, mirroring one entry of the front matter's relations array
// (document-front-matter.schema.json) and one row of document_relation.
type DocumentRelation struct {
	Kind       string
	TargetType string
	TargetID   string
}

// IndexedDocument is one row of the document table, hydrated with its tags
// and relations from their own tables.
type IndexedDocument struct {
	ID            string
	Path          string
	Title         string
	Purpose       string
	Language      string
	ContentSHA256 string
	IndexedAt     time.Time
	Tags          []string
	Relations     []DocumentRelation
}

// DocumentIssue is one row of the document_issue table: a document that
// could not be indexed, marked rather than dropped (03-arquitectura.md:85),
// with an attribution that is empty rather than absent when git could not
// answer for it — see Reindex.
type DocumentIssue struct {
	Path        string
	Reason      string
	DetectedAt  time.Time
	Attribution Attribution
}

// IndexReport summarizes one Reindex call: how many documents ended up
// indexed, and every issue recorded along the way.
type IndexReport struct {
	Indexed int
	Issues  []DocumentIssue
}

// DocumentHit is one SearchDocuments result: enough to point at a document
// and show why it matched.
type DocumentHit struct {
	DocumentID string
	Path       string
	Title      string
	Snippet    string
}

// DocumentIndexer rebuilds and queries the five document tables
// (document, document_tag, document_relation, document_issue, document_fts)
// localdb.sql gives T-022, from the markdown corpus under docs/. Nothing on
// it is exported beyond the constructor and the three readers below.
type DocumentIndexer struct {
	repo          *Repository
	db            *LocalDB
	notifications *Notifications
	attributor    Attributor
	// now is injected, like idGenerator's clock analogues elsewhere in this
	// package, so a test can fix indexed_at/detected_at instead of racing
	// time.Now() — the determinism this task is named for is only a provable
	// property if the clock is nailed down too.
	now func() time.Time
}

// NewDocumentIndexer returns a DocumentIndexer that reads docs/**/*.md under
// repo's root, stores its index in db, and reports invalid documents through
// notifications. attributor must not be nil: Reindex degrades to an empty
// Attribution whenever git cannot answer for a file (a repository with no
// commits yet, a transient failure), and that degradation is only a decision
// this package makes on purpose — never one it falls into because nobody
// wired up an attributor at all — if the constructor refuses to build
// without one.
func NewDocumentIndexer(repo *Repository, db *LocalDB, notifications *Notifications, attributor Attributor) *DocumentIndexer {
	if attributor == nil {
		panic("store: attributor must not be nil")
	}
	return &DocumentIndexer{
		repo:          repo,
		db:            db,
		notifications: notifications,
		attributor:    attributor,
		now:           time.Now,
	}
}

// walkDocuments lists every candidate document under root: files whose
// extension is .md (compared case-insensitively) anywhere below docs/. It
// reports no error and an empty list when docs/ does not exist at all — a
// project that predates the docs scaffold is a normal case, not a failure.
//
// A package variable, not a plain function, exactly so a test can inject a
// shuffled order and prove Reindex's result does not depend on it:
// normalizePaths sorts unconditionally right after this returns, and that
// sort is the property under test — removing it is what a mutation of this
// seam is supposed to catch.
var walkDocuments = defaultWalkDocuments

func defaultWalkDocuments(root string) ([]string, error) {
	docsDir := filepath.Join(root, "docs")
	info, err := os.Stat(docsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("store: checking %s: %w", docsDir, err)
	}
	if !info.IsDir() {
		// A plain file named "docs" is not the scaffold this indexes;
		// treated the same as it being absent rather than as an error.
		return nil, nil
	}

	var paths []string
	err = filepath.WalkDir(docsDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("store: walking %s: %w", docsDir, err)
	}
	return paths, nil
}

// normalizePaths turns whatever walkDocuments returned into paths relative
// to root, slash-separated (filepath.ToSlash) and sorted. Sorting here is
// the single point the whole algorithm's determinism rests on: every later
// step (collision detection, parsing order, duplicate-id "other path"
// naming, insertion order) processes candidates in the order this function
// establishes and never reorders them again, so a shuffled walkDocuments and
// the real filesystem order must converge here or nowhere.
//
// Entries may be absolute (the shape defaultWalkDocuments returns, root
// joined with "docs") or already root-relative (a shape a test's fake is
// free to use instead); both are accepted, since nothing about determinism
// depends on which one a caller of walkDocuments chooses to produce.
func normalizePaths(root string, raw []string) ([]string, error) {
	out := make([]string, len(raw))
	for i, p := range raw {
		rel := p
		if filepath.IsAbs(p) {
			r, err := filepath.Rel(root, p)
			if err != nil {
				return nil, fmt.Errorf("store: making %s relative to %s: %w", p, root, err)
			}
			rel = r
		}
		out[i] = filepath.ToSlash(rel)
	}
	sort.Strings(out)
	return out, nil
}

// documentIssueDraft accumulates what Reindex knows about one invalid
// document as it works through the algorithm's phases, before attribution
// and detection time are attached. id is "" whenever the document never
// produced a schema-valid, uniquely-owned id — a parse failure, a schema
// failure, or a path collision — which is also exactly when it can have no
// dependents: nothing else in the corpus could have named an id that was
// never established. Only a duplicate-id or dangling-relation issue carries
// a non-empty id.
type documentIssueDraft struct {
	path   string
	reason string
	id     string
}

// schemaValidCandidate is a document whose front matter parsed and passed
// document-front-matter.schema.json, before duplicate-id and relation
// checks decide whether it actually gets indexed.
type schemaValidCandidate struct {
	path          string
	id            string
	title         string
	purpose       string
	language      string
	contentSHA256 string
	tags          []string
	relations     []DocumentRelation
	body          string
}

// insertDocumentSQL inserts one row into document. A package variable, the
// same seam T-021 used for localDBSchema and T-020 used for linkFile: a test
// substitutes a broken statement to make Reindex fail partway through its
// write — after the clearing deletes have already run inside the same
// transaction — and prove the whole rebuild rolls back atomically instead of
// leaving a half-written index next to whatever notifications a partial
// write would have triggered.
var insertDocumentSQL = `insert into document (id, path, title, purpose, language, content_sha256, indexed_at) values (?, ?, ?, ?, ?, ?, ?)`

// Reindex rebuilds the document index from docs/**/*.md, in one transaction,
// and reports every document that could not be indexed. The algorithm runs
// in a fixed order because later phases depend on earlier ones:
//
//  1. Discover candidate paths (walkDocuments), normalize and sort them
//     (normalizePaths) — the single point determinism rests on.
//  2. Reject case-insensitive path collisions: on a filesystem that folds
//     case (macOS, Windows) two such paths could never have coexisted after
//     a clone, so the index cannot depend on which machine built it.
//  3. Parse and schema-validate what is left. A failure here — front matter
//     that does not parse, or does not satisfy document-front-matter.schema.json —
//     is an issue with no id: nothing about the document is trustworthy
//     enough to reuse.
//  4. Group schema-valid candidates by id. A duplicate makes every member of
//     the group an issue, naming the others, and none of them enter the
//     resolvable set below.
//  5. Fix the resolvable set: the ids of every candidate that parsed,
//     validated and was not part of a duplicate group. This set does not
//     shrink again — whether a candidate's own relations resolve has no
//     bearing on whether it can serve as *another* candidate's resolution
//     target. That is a single pass, not a fixpoint: this package's
//     04-backlog-m1.md entry names it as an explicit invariant, not an
//     incidental property of the implementation.
//  6. Check every resolvable candidate's relations: a document-type target
//     against the resolvable set itself, an artifact-type target against
//     the filesystem via Repository.checkReferences. A dangling relation
//     turns the candidate into an issue — but, per the invariant above,
//     never removes it from the resolvable set other candidates already
//     checked against.
//  7. Write everything in one transaction: clear the five document tables,
//     insert the survivors and their tags/relations/search text, insert
//     every issue.
//  8. Only after that transaction commits, emit one notification per issue —
//     never inside the transaction, which would make Notifications a second
//     writer racing the one still holding the lock.
func (x *DocumentIndexer) Reindex() (IndexReport, error) {
	raw, err := walkDocuments(x.repo.root)
	if err != nil {
		return IndexReport{}, err
	}
	paths, err := normalizePaths(x.repo.root, raw)
	if err != nil {
		return IndexReport{}, err
	}

	remaining, drafts := detectCaseCollisions(paths)

	candidates, parseIssues := x.parseAndValidateAll(remaining)
	drafts = append(drafts, parseIssues...)

	resolvable, dupIssues := groupByID(candidates)
	drafts = append(drafts, dupIssues...)

	reverseRefs := buildReverseDocumentRefs(candidates)

	// candidates is already in the one sorted order normalizePaths
	// established; iterating it here (rather than ranging over the
	// resolvable map) is what keeps insertion order — and so document_fts's
	// rowid assignment — a function of that single sort instead of Go's
	// randomized map order.
	var indexed []*schemaValidCandidate
	for _, c := range candidates {
		if resolvable[c.id] != c {
			continue // dropped above as part of a duplicate-id group
		}
		if fields := x.checkDocumentRelations(c.relations, resolvable); len(fields) > 0 {
			drafts = append(drafts, documentIssueDraft{path: c.path, id: c.id, reason: formatFieldErrors(fields)})
			continue
		}
		indexed = append(indexed, c)
	}

	now := x.now()
	issues := make([]DocumentIssue, len(drafts))
	for i, d := range drafts {
		issues[i] = DocumentIssue{
			Path:        d.path,
			Reason:      d.reason,
			DetectedAt:  now,
			Attribution: x.attribute(d.path),
		}
	}

	// document_issue's primary key is path, and every draft above came from
	// exactly one of four mutually exclusive phases keyed by path
	// (collision, parse/validate, duplicate-id, dangling-relation) — a path
	// that reached one phase's issue list was, by construction, removed from
	// the candidate set every later phase reads from. Two drafts can never
	// name the same path, so the insert loop below needs no defense against
	// a primary-key collision it cannot produce.
	if err := x.write(indexed, issues, now); err != nil {
		return IndexReport{}, err
	}

	// A failure here happens after the index has already committed, so this
	// returns an error for a Reindex whose write actually succeeded: the
	// caller cannot tell "nothing was written" from "written, but nobody was
	// told" from the returned error alone. Retrying is harmless — Notify has
	// no reason to fail on the exact same input a moment later, and Reindex
	// as a whole is naturally idempotent — but the distinction is worth this
	// one sentence rather than silence.
	if err := x.notifyIssues(drafts, issues, reverseRefs); err != nil {
		return IndexReport{}, err
	}

	return IndexReport{Indexed: len(indexed), Issues: issues}, nil
}

// detectCaseCollisions splits sortedPaths into the ones that are unique
// under strings.EqualFold and the issues naming the ones that are not. Every
// path in a fold-collision group is invalid, each naming the others: nothing
// in the algorithm has a basis to pick a winner, and picking one arbitrarily
// would make the index depend on which candidate happened to sort first.
func detectCaseCollisions(sortedPaths []string) (remaining []string, issues []documentIssueDraft) {
	groups := make(map[string][]string, len(sortedPaths))
	for _, p := range sortedPaths {
		key := strings.ToLower(p)
		groups[key] = append(groups[key], p)
	}

	for _, p := range sortedPaths {
		group := groups[strings.ToLower(p)]
		if len(group) == 1 {
			remaining = append(remaining, p)
			continue
		}
		var others []string
		for _, sibling := range group {
			if sibling != p {
				others = append(others, sibling)
			}
		}
		issues = append(issues, documentIssueDraft{
			path:   p,
			reason: fmt.Sprintf("path collides case-insensitively with %s", strings.Join(others, ", ")),
		})
	}
	return remaining, issues
}

// parseAndValidateAll reads, hashes, parses and schema-validates every path
// in sortedPaths, in order. A failure at any step becomes an issue with no
// id; a success becomes a schemaValidCandidate carrying everything later
// phases need, without re-reading the file.
func (x *DocumentIndexer) parseAndValidateAll(sortedPaths []string) (candidates []*schemaValidCandidate, issues []documentIssueDraft) {
	for _, p := range sortedPaths {
		raw, err := os.ReadFile(filepath.Join(x.repo.root, filepath.FromSlash(p)))
		if err != nil {
			issues = append(issues, documentIssueDraft{path: p, reason: fmt.Sprintf("reading file: %v", err)})
			continue
		}
		sum := sha256.Sum256(raw)

		doc, body, reason := parseFrontMatter(raw)
		if reason != "" {
			issues = append(issues, documentIssueDraft{path: p, reason: reason})
			continue
		}

		if err := x.repo.validate("document-front-matter.schema.json", p, doc); err != nil {
			issues = append(issues, documentIssueDraft{path: p, reason: validationReason(err)})
			continue
		}

		candidates = append(candidates, &schemaValidCandidate{
			path:          p,
			id:            doc.getString("id"),
			title:         doc.getString("title"),
			purpose:       doc.getString("purpose"),
			language:      doc.getString("language"),
			contentSHA256: hex.EncodeToString(sum[:]),
			tags:          decodeTags(doc["tags"]),
			relations:     decodeRelations(doc),
			body:          string(body),
		})
	}
	return candidates, issues
}

// groupByID partitions candidates (already in sorted-path order) by their
// id. A group of size one is resolvable; a larger group makes every member
// an issue naming its siblings, and none of them enter the returned map —
// picking one to keep would need a tie-break the spec does not provide.
func groupByID(candidates []*schemaValidCandidate) (resolvable map[string]*schemaValidCandidate, issues []documentIssueDraft) {
	groups := make(map[string][]*schemaValidCandidate, len(candidates))
	for _, c := range candidates {
		groups[c.id] = append(groups[c.id], c)
	}

	resolvable = make(map[string]*schemaValidCandidate, len(candidates))
	for _, c := range candidates {
		group := groups[c.id]
		if len(group) == 1 {
			resolvable[c.id] = c
			continue
		}
		var others []string
		for _, sibling := range group {
			if sibling.path != c.path {
				others = append(others, sibling.path)
			}
		}
		issues = append(issues, documentIssueDraft{
			path:   c.path,
			id:     c.id,
			reason: fmt.Sprintf("id %s is also declared by %s", c.id, strings.Join(others, ", ")),
		})
	}
	return resolvable, issues
}

// buildReverseDocumentRefs indexes, for every document-type relation target
// declared by any schema-valid candidate, the paths of the candidates that
// named it — regardless of whether either side of that relation goes on to
// be indexed successfully. It is what lets Reindex tell a notification's
// audience apart: an invalid document nobody pointed at only makes itself
// invisible, but one that other documents relied on breaks them too.
//
// candidates is walked in its already-sorted order, so each target's path
// list comes out sorted without a further pass.
func buildReverseDocumentRefs(candidates []*schemaValidCandidate) map[string][]string {
	refs := make(map[string][]string)
	for _, c := range candidates {
		named := make(map[string]bool)
		for _, rel := range c.relations {
			if rel.TargetType != "document" || named[rel.TargetID] {
				continue
			}
			named[rel.TargetID] = true
			refs[rel.TargetID] = append(refs[rel.TargetID], c.path)
		}
	}
	return refs
}

// checkDocumentRelations validates relations against resolvable (for
// document-type targets) and, for the four artifact-type targets, against
// the filesystem via Repository.checkReferences — the same function
// repository.go uses for a task's or decision's own refs[], reused here
// rather than reimplemented.
func (x *DocumentIndexer) checkDocumentRelations(relations []DocumentRelation, resolvable map[string]*schemaValidCandidate) []FieldError {
	var fields []FieldError
	var artifactRefs []reference
	for i, rel := range relations {
		field := fmt.Sprintf("relations/%d/target", i)
		if rel.TargetType == "document" {
			if _, ok := resolvable[rel.TargetID]; !ok {
				fields = append(fields, FieldError{
					Field:  field,
					Reason: fmt.Sprintf("references document %s, which does not exist", rel.TargetID),
				})
			}
			continue
		}
		artifactRefs = append(artifactRefs, reference{field: field, kind: rel.TargetType, id: rel.TargetID})
	}
	fields = append(fields, x.repo.checkReferences(artifactRefs)...)
	return fields
}

// attribute resolves who last edited path, degrading to an empty Attribution
// on any error — including the well-known case of a repository with no
// commits yet, where `git log` itself exits non-zero. This is what
// localdb.sql's empty defaults on document_issue's attribution columns exist
// for: "so that detecting an invalid document never depends on git being
// answerable at that moment."
func (x *DocumentIndexer) attribute(path string) Attribution {
	attr, err := x.attributor.LastEditor(path)
	if err != nil {
		return Attribution{}
	}
	return attr
}

// write clears the five document tables and inserts indexed and issues, all
// inside one transaction: a Reindex that fails partway through never leaves
// a half-rebuilt index in place, since a rollback undoes the clearing
// deletes just as it undoes any inserts that already ran.
//
// document is cleared first; its ON DELETE CASCADE (localdb.sql) takes
// document_tag and document_relation with it, and its AFTER DELETE trigger
// takes matching document_fts rows. document_fts is cleared explicitly on
// top of that — a row an earlier, incomplete Reindex left behind has no
// document row to be cascaded away by — and so is document_issue, which
// carries no foreign key to document at all (it is keyed by path, precisely
// so a document whose id could never be determined can still be marked).
//
// This is the ordinary db.Begin()/tx.Exec() shape, and it is safe to use
// concurrently across many Reindex calls only because localDBDSN
// (store/localdb.go) opts every connection into _txlock=immediate: without
// it, db.Begin() issues a *deferred* transaction whose write lock is
// acquired by whichever statement first needs one, and that acquisition can
// return SQLITE_BUSY outright rather than honoring busy_timeout — measured
// directly against this package's own schema, where document_fts (an FTS5
// virtual table) is enough to trigger it. See localDBDSN's comment for the
// full mechanism and notification.go's MarkRead for the same gap named from
// the read side. The fix lives in the DSN, one place for every transaction
// in the package, rather than here.
func (x *DocumentIndexer) write(indexed []*schemaValidCandidate, issues []DocumentIssue, now time.Time) error {
	db := x.db.DB()
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("store: starting document reindex: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after a successful commit is a no-op error this path cannot act on; the commit below is what is checked

	for _, stmt := range []string{
		`delete from document`,
		`delete from document_fts`,
		`delete from document_issue`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("store: clearing the document index: %w", err)
		}
	}

	indexedAt := formatTimestamp(now)
	for _, c := range indexed {
		if _, err := tx.Exec(insertDocumentSQL, c.id, c.path, c.title, c.purpose, c.language, c.contentSHA256, indexedAt); err != nil {
			return fmt.Errorf("store: indexing %s: %w", c.path, err)
		}
		for _, tag := range c.tags {
			if _, err := tx.Exec(`insert into document_tag (document_id, tag) values (?, ?)`, c.id, tag); err != nil {
				return fmt.Errorf("store: indexing tag %q of %s: %w", tag, c.path, err)
			}
		}
		for _, rel := range c.relations {
			if _, err := tx.Exec(
				`insert into document_relation (document_id, kind, target_type, target_id) values (?, ?, ?, ?)`,
				c.id, rel.Kind, rel.TargetType, rel.TargetID,
			); err != nil {
				return fmt.Errorf("store: indexing relation of %s: %w", c.path, err)
			}
		}
		// document_id is the document's ULID, never a rowid: document has a
		// TEXT primary key with no INTEGER PRIMARY KEY alias, and localdb.sql
		// documents VACUUM as free to renumber the rowids of exactly such
		// tables, which would silently repoint every search hit at the wrong
		// document. This insert is what T-021 left for T-022 to honor.
		if _, err := tx.Exec(
			`insert into document_fts (document_id, title, purpose, body) values (?, ?, ?, ?)`,
			c.id, c.title, c.purpose, c.body,
		); err != nil {
			return fmt.Errorf("store: indexing search text of %s: %w", c.path, err)
		}
	}

	for _, issue := range issues {
		if _, err := tx.Exec(
			`insert into document_issue (path, reason, detected_at, attributed_name, attributed_email, attributed_commit) values (?, ?, ?, ?, ?, ?)`,
			issue.Path, issue.Reason, formatTimestamp(issue.DetectedAt),
			issue.Attribution.Name, issue.Attribution.Email, issue.Attribution.Commit,
		); err != nil {
			return fmt.Errorf("store: recording the issue for %s: %w", issue.Path, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: committing the document reindex: %w", err)
	}
	return nil
}

// notifyIssues emits one notification per issue, after write's transaction
// has already committed — never inside it, which would make Notifications a
// second writer competing for the lock the transaction still holds. drafts
// and issues are the same length and index-aligned: drafts carries the id
// Reindex needs to look up dependents, which the exported DocumentIssue does
// not carry.
//
// The level reflects the blast radius, not just the failure: LevelWarning
// when only this document becomes invisible, LevelError when the corpus
// also loses one or more relations that pointed at it — reverseRefs answers
// which case applies. A draft with no id (a parse or schema failure, or a
// path collision) can never have dependents: nothing in the corpus could
// have named an id that was never established, so it is always
// LevelWarning. That is not free-standing reasoning — it rests on
// common.schema.json#/$defs/reference, which document-front-matter.schema.json's
// own $defs/relation.target is defined as: target.id is required and
// constrained to $defs/ulid's pattern (26 characters of Crockford base32),
// so an empty or blank target id is not a shape schema validation lets
// through at all. Without that contract, a relation could in principle name
// "" as a target, and an issue with no id of its own could coincide with one
// in reverseRefs — the guarantee here is borrowed from schemas/, not
// established by this function.
func (x *DocumentIndexer) notifyIssues(drafts []documentIssueDraft, issues []DocumentIssue, reverseRefs map[string][]string) error {
	for i, d := range drafts {
		issue := issues[i]
		dependents := reverseRefs[d.id]

		level := LevelWarning
		if len(dependents) > 0 {
			level = LevelError
		}

		title := fmt.Sprintf("Document %s is not indexable", issue.Path)
		body := formatIssueNotificationBody(issue, dependents)
		if _, err := x.notifications.Notify(level, CategoryDocument, title, body); err != nil {
			return fmt.Errorf("store: notifying about %s: %w", issue.Path, err)
		}
	}
	return nil
}

func formatIssueNotificationBody(issue DocumentIssue, dependents []string) string {
	attribution := "unknown"
	if issue.Attribution != (Attribution{}) {
		attribution = fmt.Sprintf("%s <%s> (%s)", issue.Attribution.Name, issue.Attribution.Email, issue.Attribution.Commit)
	}
	referencedBy := "none"
	if len(dependents) > 0 {
		referencedBy = strings.Join(dependents, ", ")
	}
	return fmt.Sprintf("Reason: %s\nAttribution: %s\nReferenced by: %s", issue.Reason, attribution, referencedBy)
}

// decodeTags reads a front matter document's already-schema-validated tags
// field into a sorted []string. Sorting is not required for correctness —
// document_tag's primary key is (document_id, tag), so a dump ordered by
// primary key does not care about insertion order — but it does make a
// DocumentsByTag result and this package's own tests predictable without a
// second sort at read time.
func decodeTags(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	tags := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			tags = append(tags, s)
		}
	}
	sort.Strings(tags)
	return tags
}

// decodeRelations reads a front matter document's already-schema-validated
// relations field into []DocumentRelation. The shape is guaranteed by
// document-front-matter.schema.json's own $defs/relation by the time this
// runs — kind is one of the four closed values, target is {type, id} — so
// this is a plain decode, the same trust referencesFromArray (refs.go)
// places in a document already past validation.
func decodeRelations(doc Document) []DocumentRelation {
	raw, ok := doc["relations"].([]any)
	if !ok {
		return nil
	}
	out := make([]DocumentRelation, 0, len(raw))
	for _, item := range raw {
		entry, ok := asMap(item)
		if !ok {
			continue
		}
		kind, kindOK := entry["kind"].(string)
		target, targetOK := asMap(entry["target"])
		if !kindOK || !targetOK {
			continue
		}
		targetType, typeOK := target["type"].(string)
		targetID, idOK := target["id"].(string)
		if !typeOK || !idOK {
			continue
		}
		out = append(out, DocumentRelation{Kind: kind, TargetType: targetType, TargetID: targetID})
	}
	return out
}

// formatFieldErrors joins fields the same way ArtifactValidationError.Error
// does, without repeating a path the caller already tracks separately (a
// document_issue row's own path column). Never a raw validator dump, the
// same promise store's artifact rejections make (schemas/README.md).
func formatFieldErrors(fields []FieldError) string {
	parts := make([]string, len(fields))
	for i, f := range fields {
		parts[i] = f.String()
	}
	return strings.Join(parts, "; ")
}

// validationReason extracts a repairable reason from the error
// Repository.validate returns, unwrapping *ArtifactValidationError into its
// named fields rather than falling back to Error()'s path-prefixed form.
func validationReason(err error) string {
	var verr *ArtifactValidationError
	if errors.As(err, &verr) {
		return formatFieldErrors(verr.Fields)
	}
	return err.Error()
}

// DocumentsByTag returns every indexed document carrying at least one of
// tags. It exists because T-012's closing note says a flow stage names
// "optionally, tags — which is what T-022's index knows how to query."
func (x *DocumentIndexer) DocumentsByTag(tags ...string) (results []IndexedDocument, err error) {
	if len(tags) == 0 {
		return nil, nil
	}

	db := x.db.DB()
	args := make([]any, len(tags))
	for i, t := range tags {
		args[i] = t
	}

	// Built through a strings.Builder, the same shape List (notification.go)
	// uses for its own IN clause, rather than inline "+" concatenation next
	// to the Query call: every value placeholders(len(tags)) can produce is
	// still bound as a parameter through args, never interpolated, but a
	// literal "+" directly against a query argument is indistinguishable at
	// a glance from unsafe string-built SQL, which is what gosec's G202
	// flags on sight.
	var query strings.Builder
	query.WriteString(`select distinct d.id, d.path, d.title, d.purpose, d.language, d.content_sha256, d.indexed_at
		 from document d
		 join document_tag t on t.document_id = d.id
		 where t.tag in (`)
	query.WriteString(placeholders(len(tags)))
	query.WriteString(`)
		 order by d.id`)

	rows, queryErr := db.Query(query.String(), args...)
	if queryErr != nil {
		return nil, fmt.Errorf("store: querying documents by tag: %w", queryErr)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("store: closing document rows: %w", cerr)
		}
	}()

	for rows.Next() {
		var (
			d            IndexedDocument
			indexedAtRaw string
		)
		if scanErr := rows.Scan(&d.ID, &d.Path, &d.Title, &d.Purpose, &d.Language, &d.ContentSHA256, &indexedAtRaw); scanErr != nil {
			return nil, fmt.Errorf("store: scanning document: %w", scanErr)
		}
		indexedAt, parseErr := time.Parse(time.RFC3339Nano, indexedAtRaw)
		if parseErr != nil {
			return nil, fmt.Errorf("store: document %s has an indexed_at that is not RFC 3339: %w", d.ID, parseErr)
		}
		d.IndexedAt = indexedAt
		results = append(results, d)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("store: iterating document rows: %w", rowsErr)
	}

	for i := range results {
		docTags, tagErr := x.loadTags(db, results[i].ID)
		if tagErr != nil {
			return nil, tagErr
		}
		results[i].Tags = docTags

		relations, relErr := x.loadRelations(db, results[i].ID)
		if relErr != nil {
			return nil, relErr
		}
		results[i].Relations = relations
	}
	return results, nil
}

func (x *DocumentIndexer) loadTags(db *sql.DB, id string) (tags []string, err error) {
	rows, queryErr := db.Query(`select tag from document_tag where document_id = ? order by tag`, id)
	if queryErr != nil {
		return nil, fmt.Errorf("store: loading tags for %s: %w", id, queryErr)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("store: closing tag rows: %w", cerr)
		}
	}()
	for rows.Next() {
		var tag string
		if scanErr := rows.Scan(&tag); scanErr != nil {
			return nil, fmt.Errorf("store: scanning tag: %w", scanErr)
		}
		tags = append(tags, tag)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("store: iterating tag rows: %w", rowsErr)
	}
	return tags, nil
}

func (x *DocumentIndexer) loadRelations(db *sql.DB, id string) (relations []DocumentRelation, err error) {
	rows, queryErr := db.Query(
		`select kind, target_type, target_id from document_relation where document_id = ? order by kind, target_type, target_id`,
		id,
	)
	if queryErr != nil {
		return nil, fmt.Errorf("store: loading relations for %s: %w", id, queryErr)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("store: closing relation rows: %w", cerr)
		}
	}()
	for rows.Next() {
		var r DocumentRelation
		if scanErr := rows.Scan(&r.Kind, &r.TargetType, &r.TargetID); scanErr != nil {
			return nil, fmt.Errorf("store: scanning relation: %w", scanErr)
		}
		relations = append(relations, r)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("store: iterating relation rows: %w", rowsErr)
	}
	return relations, nil
}

// SearchDocuments runs query as an FTS5 MATCH against the corpus's indexed
// title, purpose and body, newest-relevance first (FTS5's own rank column).
// It exists because a full-text index nobody can query would be write-only —
// the line of this task calls for "construcción del índice + FTS", and a
// build with no read side does not fulfill that. limit greater than zero
// caps the result count; zero or negative means no limit, the same
// convention NotificationFilter.Limit uses (notification.go).
func (x *DocumentIndexer) SearchDocuments(query string, limit int) (hits []DocumentHit, err error) {
	sqlText := `select f.document_id, d.path, f.title, snippet(document_fts, -1, '', '', '...', 10)
		from document_fts f
		join document d on d.id = f.document_id
		where document_fts match ?
		order by rank`
	args := []any{query}
	if limit > 0 {
		sqlText += ` limit ?`
		args = append(args, limit)
	}

	rows, queryErr := x.db.DB().Query(sqlText, args...)
	if queryErr != nil {
		return nil, fmt.Errorf("store: searching documents: %w", queryErr)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("store: closing search rows: %w", cerr)
		}
	}()

	for rows.Next() {
		var h DocumentHit
		if scanErr := rows.Scan(&h.DocumentID, &h.Path, &h.Title, &h.Snippet); scanErr != nil {
			return nil, fmt.Errorf("store: scanning search hit: %w", scanErr)
		}
		hits = append(hits, h)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("store: iterating search rows: %w", rowsErr)
	}
	return hits, nil
}

// Issues returns every currently-recorded document_issue row, ordered by
// path. It exists because document_issue would otherwise be write-only:
// 03-arquitectura.md:85 requires an invalid document be marked, and marking
// something nobody can read back is not that.
func (x *DocumentIndexer) Issues() (issues []DocumentIssue, err error) {
	rows, queryErr := x.db.DB().Query(
		`select path, reason, detected_at, attributed_name, attributed_email, attributed_commit
		 from document_issue order by path`,
	)
	if queryErr != nil {
		return nil, fmt.Errorf("store: listing document issues: %w", queryErr)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("store: closing document issue rows: %w", cerr)
		}
	}()

	for rows.Next() {
		var (
			issue         DocumentIssue
			detectedAtRaw string
		)
		if scanErr := rows.Scan(
			&issue.Path, &issue.Reason, &detectedAtRaw,
			&issue.Attribution.Name, &issue.Attribution.Email, &issue.Attribution.Commit,
		); scanErr != nil {
			return nil, fmt.Errorf("store: scanning document issue: %w", scanErr)
		}
		detectedAt, parseErr := time.Parse(time.RFC3339Nano, detectedAtRaw)
		if parseErr != nil {
			return nil, fmt.Errorf("store: issue for %s has a detected_at that is not RFC 3339: %w", issue.Path, parseErr)
		}
		issue.DetectedAt = detectedAt
		issues = append(issues, issue)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("store: iterating document issue rows: %w", rowsErr)
	}
	return issues, nil
}
