package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- fixtures ----------------------------------------------------------------

// fakeAttributor is a deterministic Attributor for tests: per-path results
// and errors configured up front, no git binary involved. A path with
// neither configured returns a zero Attribution and no error — most tests
// do not care about attribution, and only the ones that do configure it.
type fakeAttributor struct {
	results map[string]Attribution
	errs    map[string]error
}

func newFakeAttributor() *fakeAttributor {
	return &fakeAttributor{results: map[string]Attribution{}, errs: map[string]error{}}
}

func (f *fakeAttributor) LastEditor(path string) (Attribution, error) {
	if err, ok := f.errs[path]; ok {
		return Attribution{}, err
	}
	if attr, ok := f.results[path]; ok {
		return attr, nil
	}
	return Attribution{}, nil
}

// indexerFixture bundles a DocumentIndexer with the pieces a test needs to
// inspect or drive it directly: repo and db share one root, the pitfall
// newTestRepository(t) alone would hit (it mints its own t.TempDir(), a
// different directory from any separately-opened LocalDB).
type indexerFixture struct {
	indexer       *DocumentIndexer
	root          string
	ldb           *LocalDB
	repo          *Repository
	attributor    *fakeAttributor
	notifications *Notifications
}

var fixedIndexTime = time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)

func newIndexerFixture(t *testing.T) *indexerFixture {
	t.Helper()

	ldb, root := newOpenedLocalDB(t)
	repo, err := Open(root, testIdentity)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	notifications := NewNotifications(ldb)
	attributor := newFakeAttributor()
	indexer := NewDocumentIndexer(repo, ldb, notifications, attributor)
	indexer.now = func() time.Time { return fixedIndexTime }

	return &indexerFixture{
		indexer:       indexer,
		root:          root,
		ldb:           ldb,
		repo:          repo,
		attributor:    attributor,
		notifications: notifications,
	}
}

// writeDocFile writes content at root/relPath (slash-separated), creating
// parent directories as needed.
func writeDocFile(t *testing.T, root, relPath, content string) {
	t.Helper()

	full := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatalf("creating %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", full, err)
	}
}

// buildFrontMatterFile assembles a minimal, schema-valid front-matter
// markdown file. extraYAML, if non-empty, is inserted as additional
// top-level lines (tags, relations) before the closing delimiter; it must
// end in its own newline.
func buildFrontMatterFile(id, title, purpose, language, extraYAML, body string) string {
	return "---\n" +
		"schemaVersion: 1\n" +
		"id: " + id + "\n" +
		"title: " + title + "\n" +
		"purpose: " + purpose + "\n" +
		"language: " + language + "\n" +
		extraYAML +
		"---\n" +
		body
}

func documentRelationYAML(kind, targetType, targetID string) string {
	return fmt.Sprintf("relations:\n  - kind: %s\n    target: {type: %s, id: %s}\n", kind, targetType, targetID)
}

// scanStrings scans exactly n text columns of the current row into strings,
// which is every column dumpDocumentTables cares about once rowid has been
// cast to text in the query itself.
func scanStrings(t *testing.T, rows *sql.Rows, n int) []string {
	t.Helper()

	vals := make([]string, n)
	ptrs := make([]any, n)
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		t.Fatalf("scanning row: %v", err)
	}
	return vals
}

// dumpDocumentTables renders the five document tables as text, each ordered
// by its own primary key, document_fts included with its rowid cast to
// text. Comparing two dumps this way is blind to insertion order for every
// column except that rowid one — which is exactly the point: rowid reflects
// insertion order (verified empirically against modernc.org/sqlite before
// relying on it here), so two Reindex runs that process the same corpus in a
// different order produce identical dumps only if that order converges
// before anything is written — i.e., only if normalizePaths's sort actually
// ran.
func dumpDocumentTables(t *testing.T, db *sql.DB) string {
	t.Helper()

	var b strings.Builder
	appendRows := func(label, query string, n int) {
		b.WriteString(label + ":\n")
		rows, err := db.Query(query)
		if err != nil {
			t.Fatalf("querying %s: %v", label, err)
		}
		for rows.Next() {
			b.WriteString("  " + strings.Join(scanStrings(t, rows, n), "|") + "\n")
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterating %s: %v", label, err)
		}
		if err := rows.Close(); err != nil {
			t.Fatalf("closing %s rows: %v", label, err)
		}
	}

	appendRows("document", `select id, path, title, purpose, language, content_sha256, indexed_at from document order by id`, 7)
	appendRows("document_tag", `select document_id, tag from document_tag order by document_id, tag`, 2)
	appendRows("document_relation", `select document_id, kind, target_type, target_id from document_relation order by document_id, kind, target_type, target_id`, 4)
	appendRows("document_issue", `select path, reason, detected_at, attributed_name, attributed_email, attributed_commit from document_issue order by path`, 6)
	appendRows("document_fts", `select cast(rowid as text), document_id, title, purpose, body from document_fts order by document_id`, 5)
	return b.String()
}

func TestNewDocumentIndexer_PanicsOnNilAttributor(t *testing.T) {
	ldb, root := newOpenedLocalDB(t)
	repo, err := Open(root, testIdentity)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	notifications := NewNotifications(ldb)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NewDocumentIndexer did not panic with a nil attributor")
		}
	}()
	NewDocumentIndexer(repo, ldb, notifications, nil)
}

// --- determinism ---------------------------------------------------------

// TestDocumentIndexer_Reindex_IsDeterministic is this task's flagship
// property: indexing the same corpus twice, on clean databases, with the
// clock fixed, produces byte-identical dumps of the five document tables —
// regardless of the order walkDocuments happened to return paths in.
func TestDocumentIndexer_Reindex_IsDeterministic(t *testing.T) {
	idAlpha := mustNewULID(t)
	idBeta := mustNewULID(t)
	idGamma := mustNewULID(t)

	root := t.TempDir()
	writeDocFile(t, root, "docs/alpha.md", buildFrontMatterFile(idAlpha, "Alpha", "About alpha.", "en",
		"tags: [x, y]\n"+documentRelationYAML("relates_to", "document", idBeta), "Alpha body.\n"))
	writeDocFile(t, root, "docs/beta.md", buildFrontMatterFile(idBeta, "Beta", "About beta.", "en", "", "Beta body.\n"))
	writeDocFile(t, root, "docs/nested/gamma.md", buildFrontMatterFile(idGamma, "Gamma", "About gamma.", "en", "", "Gamma body.\n"))

	// A three-way duplicate-id group, not a two-way one: with only two
	// members, the "also declared by <the other path>" reason has exactly
	// one candidate for "the other path" regardless of processing order, so
	// it cannot expose a missing sort. With three, groupByID's "others" list
	// has two entries whose relative order tracks processing order — the
	// same order a missing sort would corrupt — which is what makes
	// document_issue's content, and not only document_fts's rowids, part of
	// what this test's mutation-sensitivity rests on.
	dupID := mustNewULID(t)
	writeDocFile(t, root, "docs/dup1.md", buildFrontMatterFile(dupID, "Dup1", "About dup1.", "en", "", "Dup1 body.\n"))
	writeDocFile(t, root, "docs/dup2.md", buildFrontMatterFile(dupID, "Dup2", "About dup2.", "en", "", "Dup2 body.\n"))
	writeDocFile(t, root, "docs/dup3.md", buildFrontMatterFile(dupID, "Dup3", "About dup3.", "en", "", "Dup3 body.\n"))

	run := func(walker func(string) ([]string, error)) string {
		ldb, dbRoot := newOpenedLocalDB(t)
		_ = dbRoot // the database lives in its own temp dir; the corpus lives in root
		repo, err := Open(root, testIdentity)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		notifications := NewNotifications(ldb)
		indexer := NewDocumentIndexer(repo, ldb, notifications, newFakeAttributor())
		indexer.now = func() time.Time { return fixedIndexTime }

		if walker != nil {
			original := walkDocuments
			walkDocuments = walker
			t.Cleanup(func() { walkDocuments = original })
		}

		if _, err := indexer.Reindex(); err != nil {
			t.Fatalf("Reindex: %v", err)
		}
		return dumpDocumentTables(t, ldb.DB())
	}

	baseline := run(nil)

	reversedWalk := func(r string) ([]string, error) {
		paths, err := defaultWalkDocuments(r)
		if err != nil {
			return nil, err
		}
		reversed := make([]string, len(paths))
		for i, p := range paths {
			reversed[len(paths)-1-i] = p
		}
		return reversed, nil
	}
	reversed := run(reversedWalk)

	if baseline != reversed {
		t.Fatalf("dump with a reversed walkDocuments order differs from the natural-order baseline:\n--- baseline ---\n%s--- reversed ---\n%s", baseline, reversed)
	}
}

// --- basic algorithm phases -------------------------------------------------

func TestDocumentIndexer_Reindex_DocsDirectoryAbsent(t *testing.T) {
	f := newIndexerFixture(t)

	report, err := f.indexer.Reindex()
	if err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	if report.Indexed != 0 || len(report.Issues) != 0 {
		t.Fatalf("report = %+v, want an empty report", report)
	}
}

func TestDocumentIndexer_Reindex_HappyPathWithTagsAndRelations(t *testing.T) {
	f := newIndexerFixture(t)

	taskID, _, err := f.repo.CreateTask(validTaskBusiness())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	idAlpha := mustNewULID(t)
	idBeta := mustNewULID(t)

	extra := "tags: [platform, indexing]\n" +
		"relations:\n" +
		"  - kind: relates_to\n" +
		"    target: {type: document, id: " + idBeta + "}\n" +
		"  - kind: implements\n" +
		"    target: {type: task, id: " + taskID + "}\n"
	writeDocFile(t, f.root, "docs/alpha.md", buildFrontMatterFile(idAlpha, "Alpha document", "Alpha purpose text.", "en", extra, "Alpha body mentions indexing.\n"))
	writeDocFile(t, f.root, "docs/beta.md", buildFrontMatterFile(idBeta, "Beta document", "Beta purpose text.", "en", "", "Beta body.\n"))

	report, err := f.indexer.Reindex()
	if err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	if report.Indexed != 2 {
		t.Fatalf("Indexed = %d, want 2", report.Indexed)
	}
	if len(report.Issues) != 0 {
		t.Fatalf("Issues = %+v, want none", report.Issues)
	}

	byTag, err := f.indexer.DocumentsByTag("platform")
	if err != nil {
		t.Fatalf("DocumentsByTag: %v", err)
	}
	if len(byTag) != 1 || byTag[0].ID != idAlpha {
		t.Fatalf("DocumentsByTag(platform) = %+v, want just alpha", byTag)
	}
	if len(byTag[0].Tags) != 2 || byTag[0].Tags[0] != "indexing" || byTag[0].Tags[1] != "platform" {
		t.Fatalf("Tags = %v, want sorted [indexing platform]", byTag[0].Tags)
	}
	if len(byTag[0].Relations) != 2 {
		t.Fatalf("Relations = %+v, want 2", byTag[0].Relations)
	}

	hits, err := f.indexer.SearchDocuments("indexing", 0)
	if err != nil {
		t.Fatalf("SearchDocuments: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("SearchDocuments(indexing) returned no hits")
	}
	found := false
	for _, h := range hits {
		if h.DocumentID == idAlpha {
			found = true
			if h.Path != "docs/alpha.md" {
				t.Fatalf("hit.Path = %q, want docs/alpha.md", h.Path)
			}
			if h.Snippet == "" {
				t.Fatal("hit.Snippet is empty")
			}
		}
	}
	if !found {
		t.Fatalf("SearchDocuments(indexing) did not include alpha: %+v", hits)
	}

	issues, err := f.indexer.Issues()
	if err != nil {
		t.Fatalf("Issues: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("Issues() = %+v, want none", issues)
	}
}

func TestDocumentIndexer_Reindex_DanglingArtifactRelation(t *testing.T) {
	f := newIndexerFixture(t)

	nonexistentTask := mustNewULID(t)
	id := mustNewULID(t)
	extra := documentRelationYAML("implements", "task", nonexistentTask)
	writeDocFile(t, f.root, "docs/orphan.md", buildFrontMatterFile(id, "Orphan", "Purpose.", "en", extra, "Body.\n"))

	report, err := f.indexer.Reindex()
	if err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	if report.Indexed != 0 {
		t.Fatalf("Indexed = %d, want 0", report.Indexed)
	}
	if len(report.Issues) != 1 {
		t.Fatalf("Issues = %+v, want exactly 1", report.Issues)
	}
	issue := report.Issues[0]
	if issue.Path != "docs/orphan.md" {
		t.Fatalf("issue.Path = %q", issue.Path)
	}
	if !strings.Contains(issue.Reason, nonexistentTask) || !strings.Contains(issue.Reason, "does not exist") {
		t.Fatalf("issue.Reason = %q, want it to name the dangling task", issue.Reason)
	}
}

func TestDocumentIndexer_Reindex_DuplicateID(t *testing.T) {
	f := newIndexerFixture(t)

	sharedID := mustNewULID(t)
	writeDocFile(t, f.root, "docs/one.md", buildFrontMatterFile(sharedID, "One", "Purpose one.", "en", "", "Body one.\n"))
	writeDocFile(t, f.root, "docs/two.md", buildFrontMatterFile(sharedID, "Two", "Purpose two.", "en", "", "Body two.\n"))

	report, err := f.indexer.Reindex()
	if err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	if report.Indexed != 0 {
		t.Fatalf("Indexed = %d, want 0", report.Indexed)
	}
	if len(report.Issues) != 2 {
		t.Fatalf("Issues = %+v, want exactly 2", report.Issues)
	}

	byPath := map[string]DocumentIssue{}
	for _, iss := range report.Issues {
		byPath[iss.Path] = iss
	}
	one, ok := byPath["docs/one.md"]
	if !ok {
		t.Fatalf("no issue for docs/one.md: %+v", report.Issues)
	}
	if !strings.Contains(one.Reason, "docs/two.md") {
		t.Fatalf("one.Reason = %q, want it to name docs/two.md", one.Reason)
	}
	two, ok := byPath["docs/two.md"]
	if !ok {
		t.Fatalf("no issue for docs/two.md: %+v", report.Issues)
	}
	if !strings.Contains(two.Reason, "docs/one.md") {
		t.Fatalf("two.Reason = %q, want it to name docs/one.md", two.Reason)
	}
}

// TestDocumentIndexer_Reindex_CaseCollision drives the collision through the
// walkDocuments seam rather than two real files on disk: this development
// machine's filesystem (like Windows, and unlike Linux) folds case, so
// writing "docs/Foo.md" and then "docs/foo.md" would collapse into a single
// directory entry before Reindex ever saw two candidates — exactly the
// platform difference this check exists to guard against, which means it
// cannot be exercised by relying on the local filesystem to preserve both
// names. detectCaseCollisions runs on the path strings alone, before either
// file is read, so injecting the two paths directly still exercises the
// real code path end to end.
func TestDocumentIndexer_Reindex_CaseCollision(t *testing.T) {
	f := newIndexerFixture(t)

	original := walkDocuments
	walkDocuments = func(root string) ([]string, error) {
		return []string{
			filepath.Join(root, "docs", "Foo.md"),
			filepath.Join(root, "docs", "foo.md"),
		}, nil
	}
	t.Cleanup(func() { walkDocuments = original })

	report, err := f.indexer.Reindex()
	if err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	if report.Indexed != 0 {
		t.Fatalf("Indexed = %d, want 0", report.Indexed)
	}
	if len(report.Issues) != 2 {
		t.Fatalf("Issues = %+v, want exactly 2", report.Issues)
	}
	for _, iss := range report.Issues {
		if !strings.Contains(iss.Reason, "collides case-insensitively") {
			t.Fatalf("issue.Reason = %q, want it to mention the case collision", iss.Reason)
		}
	}
}

func TestDocumentIndexer_Reindex_MissingFrontMatter(t *testing.T) {
	f := newIndexerFixture(t)

	writeDocFile(t, f.root, "docs/plain.md", "# Just a heading\n\nNo front matter here.\n")

	report, err := f.indexer.Reindex()
	if err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	if report.Indexed != 0 || len(report.Issues) != 1 {
		t.Fatalf("report = %+v, want 0 indexed and 1 issue", report)
	}
	if !strings.Contains(report.Issues[0].Reason, "missing front matter") {
		t.Fatalf("Reason = %q", report.Issues[0].Reason)
	}
}

func TestDocumentIndexer_Reindex_RemovesStaleEntries(t *testing.T) {
	f := newIndexerFixture(t)

	idA := mustNewULID(t)
	idB := mustNewULID(t)
	writeDocFile(t, f.root, "docs/a.md", buildFrontMatterFile(idA, "A", "Purpose A.", "en", "", "Body A.\n"))
	betaPath := filepath.Join(f.root, "docs", "b.md")
	writeDocFile(t, f.root, "docs/b.md", buildFrontMatterFile(idB, "B", "Purpose B.", "en", "", "Body B.\n"))

	if report, err := f.indexer.Reindex(); err != nil || report.Indexed != 2 {
		t.Fatalf("first Reindex: report=%+v err=%v", report, err)
	}

	if err := os.Remove(betaPath); err != nil {
		t.Fatalf("removing %s: %v", betaPath, err)
	}

	report, err := f.indexer.Reindex()
	if err != nil {
		t.Fatalf("second Reindex: %v", err)
	}
	if report.Indexed != 1 {
		t.Fatalf("Indexed = %d, want 1", report.Indexed)
	}

	if n := countRows(t, f.ldb.DB(), "document"); n != 1 {
		t.Fatalf("document has %d rows, want 1", n)
	}
	if n := countRows(t, f.ldb.DB(), "document_fts"); n != 1 {
		t.Fatalf("document_fts has %d rows, want 1", n)
	}
	var remainingID string
	if err := f.ldb.DB().QueryRow(`select id from document`).Scan(&remainingID); err != nil {
		t.Fatalf("reading remaining document id: %v", err)
	}
	if remainingID != idA {
		t.Fatalf("remaining document id = %s, want %s", remainingID, idA)
	}
}

func TestDocumentIndexer_Reindex_ClearsOrphanFTSRow(t *testing.T) {
	f := newIndexerFixture(t)

	orphanID := mustNewULID(t)
	if _, err := f.ldb.DB().Exec(
		`insert into document_fts (document_id, title, purpose, body) values (?, ?, ?, ?)`,
		orphanID, "Orphan title", "Orphan purpose", "Orphan body",
	); err != nil {
		t.Fatalf("inserting orphan document_fts row: %v", err)
	}

	if _, err := f.indexer.Reindex(); err != nil {
		t.Fatalf("Reindex: %v", err)
	}

	var count int
	if err := f.ldb.DB().QueryRow(`select count(*) from document_fts where document_id = ?`, orphanID).Scan(&count); err != nil {
		t.Fatalf("checking for the orphan row: %v", err)
	}
	if count != 0 {
		t.Fatalf("orphan document_fts row for %s survived Reindex", orphanID)
	}
}

// TestDocumentIndexer_Reindex_FTSTracksDocumentByID builds a corpus where
// path-sorted processing order (a.md before z.md) is the *opposite* of
// id-sorted order (z's id was minted first, so it sorts first as a ULID):
// a.md is inserted first and lands on document_fts's first rowid, z.md
// second. A follow-up Reindex that drops a.md from disk then has to prove
// deletion tracks document_id, not the row's old position — the only thing
// a rowid-keyed mistake could get wrong.
func TestDocumentIndexer_Reindex_FTSTracksDocumentByID(t *testing.T) {
	f := newIndexerFixture(t)

	// mustNewULID builds a fresh idGenerator per call, so two calls are not
	// guaranteed to sort in call order (only a shared generator's monotonic
	// entropy promises that) — sort the pair explicitly instead of assuming
	// it, so idForZ < idForA always holds regardless.
	idForZ, idForA := mustNewULID(t), mustNewULID(t)
	if idForZ > idForA {
		idForZ, idForA = idForA, idForZ
	}

	aliceRelPath := "docs/a.md"
	writeDocFile(t, f.root, aliceRelPath, buildFrontMatterFile(idForA, "A document", "Purpose A.", "en", "", "Body A.\n"))
	writeDocFile(t, f.root, "docs/z.md", buildFrontMatterFile(idForZ, "Z document", "Purpose Z.", "en", "", "Body Z.\n"))

	if report, err := f.indexer.Reindex(); err != nil || report.Indexed != 2 {
		t.Fatalf("first Reindex: report=%+v err=%v", report, err)
	}

	if err := os.Remove(filepath.Join(f.root, filepath.FromSlash(aliceRelPath))); err != nil {
		t.Fatalf("removing %s: %v", aliceRelPath, err)
	}
	if report, err := f.indexer.Reindex(); err != nil || report.Indexed != 1 {
		t.Fatalf("second Reindex: report=%+v err=%v", report, err)
	}

	var count int
	if err := f.ldb.DB().QueryRow(`select count(*) from document_fts`).Scan(&count); err != nil {
		t.Fatalf("counting document_fts: %v", err)
	}
	if count != 1 {
		t.Fatalf("document_fts has %d rows, want exactly 1", count)
	}

	var docID, title string
	if err := f.ldb.DB().QueryRow(`select document_id, title from document_fts`).Scan(&docID, &title); err != nil {
		t.Fatalf("reading the surviving row: %v", err)
	}
	if docID != idForZ || title != "Z document" {
		t.Fatalf("surviving row = (%s, %s), want (%s, Z document)", docID, title, idForZ)
	}
}

// --- attribution and notifications ------------------------------------------

func TestDocumentIndexer_Reindex_AttributionFailureDegradesGracefully(t *testing.T) {
	f := newIndexerFixture(t)
	f.attributor.errs["docs/broken.md"] = errors.New("git log failed: no commits yet")

	writeDocFile(t, f.root, "docs/broken.md", "not front matter\n")

	report, err := f.indexer.Reindex()
	if err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	if len(report.Issues) != 1 {
		t.Fatalf("Issues = %+v, want exactly 1", report.Issues)
	}
	if report.Issues[0].Attribution != (Attribution{}) {
		t.Fatalf("Attribution = %+v, want the zero value on attributor failure", report.Issues[0].Attribution)
	}
}

func TestDocumentIndexer_Reindex_AttributionSucceeds(t *testing.T) {
	f := newIndexerFixture(t)
	want := Attribution{Commit: strings.Repeat("a", 40), Name: "Ada Lovelace", Email: "ada@example.com"}
	f.attributor.results["docs/broken.md"] = want

	writeDocFile(t, f.root, "docs/broken.md", "not front matter\n")

	if _, err := f.indexer.Reindex(); err != nil {
		t.Fatalf("Reindex: %v", err)
	}

	issues, err := f.indexer.Issues()
	if err != nil {
		t.Fatalf("Issues: %v", err)
	}
	if len(issues) != 1 || issues[0].Attribution != want {
		t.Fatalf("Issues() = %+v, want Attribution = %+v", issues, want)
	}
}

func TestDocumentIndexer_Reindex_EmitsOneNotificationPerIssue(t *testing.T) {
	f := newIndexerFixture(t)
	writeDocFile(t, f.root, "docs/bad-one.md", "no front matter\n")
	writeDocFile(t, f.root, "docs/bad-two.md", "also no front matter\n")

	if _, err := f.indexer.Reindex(); err != nil {
		t.Fatalf("Reindex: %v", err)
	}

	notifs, err := f.notifications.List(NotificationFilter{Categories: []NotificationCategory{CategoryDocument}})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(notifs) != 2 {
		t.Fatalf("notifications = %+v, want exactly 2", notifs)
	}
	for _, n := range notifs {
		if n.Level != LevelWarning {
			t.Fatalf("notification level = %s, want warning (neither document has dependents)", n.Level)
		}
	}
}

// TestDocumentIndexer_Reindex_LevelReflectsDependents builds the one corpus
// shape that actually exercises LevelError, and — as importantly — the one
// that can actually catch a cascade if the "single pass, no cascade"
// invariant (Reindex's own doc comment) were ever broken: the target with a
// dangling relation of its own must sort *before* the document that
// references it, not after.
//
// The reason that ordering matters: Reindex's relation-check loop walks
// candidates in the one sorted order normalizePaths establishes, and never
// revisits a candidate once decided. If the referencer were checked first —
// the natural-looking "A points at B" order, A before B — a buggy
// implementation that removes an invalidated candidate from resolvable
// (deleteResolvable(c.id) the moment its own relations fail) would still
// pass this test: the referencer's relation to the target gets checked
// *before* the target is ever invalidated, so resolvable still has the
// target's id at that point regardless of whether the cascade exists. A
// test with that ordering could report the invariant true and still be
// exercising nothing. This was caught in review by injecting exactly that
// cascade and finding the whole store suite, this test included, still
// green.
//
// So the target (docs/aa-target.md) is named to sort before the referencer
// (docs/zz-referencer.md): by the time the referencer's own relation is
// checked, a cascading implementation would already have dropped the
// target's id from resolvable, and the referencer would wrongly become an
// issue too — changing Indexed from 1 to 0. A third, lone invalid document
// with no dependents proves the LevelWarning side of the same comparison.
func TestDocumentIndexer_Reindex_LevelReflectsDependents(t *testing.T) {
	f := newIndexerFixture(t)

	idTarget := mustNewULID(t)
	nonexistentTask := mustNewULID(t)
	writeDocFile(t, f.root, "docs/aa-target.md", buildFrontMatterFile(idTarget, "Target", "Purpose target.", "en",
		documentRelationYAML("implements", "task", nonexistentTask), "Body target.\n"))

	idReferencer := mustNewULID(t)
	writeDocFile(t, f.root, "docs/zz-referencer.md", buildFrontMatterFile(idReferencer, "Referencer", "Purpose referencer.", "en",
		documentRelationYAML("relates_to", "document", idTarget), "Body referencer.\n"))

	// lonely.md is invalid for the same reason aa-target.md is — a dangling
	// relation to a task nobody declared — but no other document names it as
	// a target, which is what should keep its notification at LevelWarning.
	idLonely := mustNewULID(t)
	nonexistentTask2 := mustNewULID(t)
	writeDocFile(t, f.root, "docs/lonely.md", buildFrontMatterFile(idLonely, "Lonely", "Purpose lonely.", "en",
		documentRelationYAML("implements", "task", nonexistentTask2), "Body lonely.\n"))

	report, err := f.indexer.Reindex()
	if err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	if report.Indexed != 1 {
		t.Fatalf("Indexed = %d, want 1 (only the referencer)", report.Indexed)
	}
	if len(report.Issues) != 2 {
		t.Fatalf("Issues = %+v, want exactly 2 (the target and lonely)", report.Issues)
	}

	notifs, err := f.notifications.List(NotificationFilter{Categories: []NotificationCategory{CategoryDocument}})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(notifs) != 2 {
		t.Fatalf("notifications = %+v, want exactly 2", notifs)
	}

	var targetNotif, lonelyNotif *Notification
	for i := range notifs {
		switch {
		case strings.Contains(notifs[i].Title, "docs/aa-target.md"):
			targetNotif = &notifs[i]
		case strings.Contains(notifs[i].Title, "docs/lonely.md"):
			lonelyNotif = &notifs[i]
		}
	}
	if targetNotif == nil || lonelyNotif == nil {
		t.Fatalf("notifications did not name both aa-target.md and lonely.md: %+v", notifs)
	}
	if targetNotif.Level != LevelError {
		t.Fatalf("target notification level = %s, want error (docs/zz-referencer.md depends on it)", targetNotif.Level)
	}
	if !strings.Contains(targetNotif.Body, "docs/zz-referencer.md") {
		t.Fatalf("target notification body = %q, want it to name docs/zz-referencer.md as a dependent", targetNotif.Body)
	}
	if lonelyNotif.Level != LevelWarning {
		t.Fatalf("lonely.md notification level = %s, want warning (nothing depends on it)", lonelyNotif.Level)
	}
}

// --- transactionality -------------------------------------------------------

// TestDocumentIndexer_Reindex_FailurePartwayLeavesPreviousIndexIntact covers
// two properties together, since both need the same corpus shape (at least
// one indexable document, so insertDocumentSQL actually runs, and at least
// one issue, so a notification would have been emitted had the write
// succeeded): a Reindex that fails partway through its transaction leaves
// whatever the previous successful Reindex wrote completely untouched
// (rollback undoes the clearing deletes along with any partial inserts —
// there is no observable "half rebuilt" state), and no notification is ever
// emitted for a Reindex whose transaction did not commit.
func TestDocumentIndexer_Reindex_FailurePartwayLeavesPreviousIndexIntact(t *testing.T) {
	f := newIndexerFixture(t)

	goodID := mustNewULID(t)
	writeDocFile(t, f.root, "docs/good.md", buildFrontMatterFile(goodID, "Good", "Purpose.", "en", "", "Body.\n"))
	writeDocFile(t, f.root, "docs/bad.md", "no front matter\n")

	first, err := f.indexer.Reindex()
	if err != nil {
		t.Fatalf("first Reindex: %v", err)
	}
	if first.Indexed != 1 || len(first.Issues) != 1 {
		t.Fatalf("first report = %+v, want 1 indexed and 1 issue", first)
	}
	beforeDump := dumpDocumentTables(t, f.ldb.DB())
	beforeNotifs, err := f.notifications.List(NotificationFilter{})
	if err != nil {
		t.Fatalf("List before: %v", err)
	}

	original := insertDocumentSQL
	insertDocumentSQL = `insert into document (id, path, title, purpose, language, content_sha256) values (?, ?, ?, ?, ?, ?)` // wrong arity: forces a failure after the clearing deletes have already run
	t.Cleanup(func() { insertDocumentSQL = original })

	if _, err := f.indexer.Reindex(); err == nil {
		t.Fatal("second Reindex succeeded against a broken insertDocumentSQL, want an error")
	}

	afterDump := dumpDocumentTables(t, f.ldb.DB())
	if beforeDump != afterDump {
		t.Fatalf("a failed Reindex changed the committed state:\n--- before ---\n%s--- after ---\n%s", beforeDump, afterDump)
	}

	afterNotifs, err := f.notifications.List(NotificationFilter{})
	if err != nil {
		t.Fatalf("List after: %v", err)
	}
	if len(afterNotifs) != len(beforeNotifs) {
		t.Fatalf("notification count changed from %d to %d after a failed Reindex", len(beforeNotifs), len(afterNotifs))
	}
}

// --- concurrency -------------------------------------------------------------

func TestDocumentIndexer_Reindex_ConcurrentCalls(t *testing.T) {
	f := newIndexerFixture(t)
	writeDocFile(t, f.root, "docs/one.md", buildFrontMatterFile(mustNewULID(t), "One", "Purpose.", "en", "", "Body.\n"))
	writeDocFile(t, f.root, "docs/two.md", buildFrontMatterFile(mustNewULID(t), "Two", "Purpose.", "en", "", "Body.\n"))

	const goroutines = 6
	var wg sync.WaitGroup
	errs := make([]error, goroutines)
	for i := range goroutines {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := f.indexer.Reindex()
			errs[i] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: Reindex: %v", i, err)
		}
	}

	// The database must be left in a state some single Reindex call could
	// have produced: exactly the two documents, never a partial mix.
	if n := countRows(t, f.ldb.DB(), "document"); n != 2 {
		t.Fatalf("document has %d rows after concurrent Reindex calls, want 2", n)
	}

	var wgReads sync.WaitGroup
	for range goroutines {
		wgReads.Add(1)
		go func() {
			defer wgReads.Done()
			if _, err := f.indexer.SearchDocuments("Body", 0); err != nil {
				t.Errorf("SearchDocuments: %v", err)
			}
			if _, err := f.indexer.Issues(); err != nil {
				t.Errorf("Issues: %v", err)
			}
		}()
	}
	wgReads.Wait()
}
