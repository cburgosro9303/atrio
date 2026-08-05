package store

import (
	"fmt"
	"os"
	"path/filepath"
)

// reference is a decoded common.schema.json#/$defs/reference: a typed
// pointer from one artifact at another.
type reference struct {
	// field is where this reference sits in the document it came from
	// (e.g. "refs/0" or "stages/1/outputRefs/0"), used to name it in a
	// rejection.
	field string
	kind  string
	id    string
}

// resolvableRefDirs maps the artifactType values this repository can check
// by simple file existence to the folder that holds them. "document" is
// deliberately absent: a document lives in docs/**/*.md and whether a given
// id resolves to one is answered by the documental index, not by a stat —
// that half of referential integrity belongs to T-022
// (schemas/README.md's "qué valida el esquema y qué valida el código" table).
var resolvableRefDirs = map[string]string{
	"task":      taskKind.dir,
	"decision":  decisionKind.dir,
	"log_entry": logEntryKind.dir,
	"changelog": changelogKind.dir,
}

// checkReferences confirms that every reference this repository can resolve
// by file existence actually points at a file under this project's
// management folder. It is called after schema validation succeeds, so
// every reference here is already known to have the right shape
// ({type, id} with a well-formed ulid) — what is left to check is whether
// the artifact it names exists. A nil result means every resolvable
// reference checked out.
func (r *Repository) checkReferences(refs []reference) []FieldError {
	var fields []FieldError
	for _, ref := range refs {
		dir, resolvable := resolvableRefDirs[ref.kind]
		if !resolvable {
			continue
		}
		path := filepath.Join(r.root, managementDir, dir, ref.id+".json")
		if _, err := os.Stat(path); err != nil {
			fields = append(fields, FieldError{
				Field:  ref.field,
				Reason: fmt.Sprintf("references %s %s, which does not exist", ref.kind, ref.id),
			})
		}
	}
	return fields
}

// referencesFromArray decodes the value of a field holding an array of
// common.schema.json#/$defs/reference — {type, id} — into references named
// after their position under fieldPrefix (e.g. "refs" -> "refs/0", "refs/1").
// Anything not shaped that way is skipped: by the time this runs, schema
// validation has already guaranteed the shape, so a mismatch here can only
// mean the field was absent, which is not this function's concern.
func referencesFromArray(value any, fieldPrefix string) []reference {
	items, ok := value.([]any)
	if !ok {
		return nil
	}

	refs := make([]reference, 0, len(items))
	for i, item := range items {
		entry, ok := asMap(item)
		if !ok {
			continue
		}
		kind, kindOK := entry["type"].(string)
		id, idOK := entry["id"].(string)
		if !kindOK || !idOK || kind == "" || id == "" {
			continue
		}
		refs = append(refs, reference{
			field: fmt.Sprintf("%s/%d", fieldPrefix, i),
			kind:  kind,
			id:    id,
		})
	}
	return refs
}
