package store

// Document is one artifact's JSON body, decoded generically. store persists
// and validates artifacts against their JSON Schema without needing to know
// their full business shape: modeling that shape as Go types belongs to
// core (T-040) and to the later consumers that build on this repository
// (T-070/T-080). A Document is exactly what schema.Validate accepts and
// what json.Marshal turns back into the bytes written to disk.
//
// A caller building the business fields passed into a Create/Update/Write
// call is free to nest either Document or plain map[string]any (plainValue,
// in this file, normalizes both before validation). What a caller gets back
// is a different matter: every Create/Update/Write in this package re-reads
// the file it just wrote before returning it (repository.go), so its
// nested objects are always plain map[string]any and its nested numbers are
// always json.Number — exactly what a later Read call would decode, via
// jsonschema.UnmarshalJSON, from the same file. That is deliberate: a type
// assertion written against a freshly created document must keep working
// unchanged against that same document read back later.
type Document map[string]any

// clone makes a shallow copy of d. The repository uses this to avoid a
// caller's map being mutated out from under it when the repository stamps
// envelope fields onto a copy before validating and writing.
func (d Document) clone() Document {
	out := make(Document, len(d))
	for k, v := range d {
		out[k] = v
	}
	return out
}

// getString reads a string field, returning "" for anything absent or of a
// different type. It is used for the handful of fields the repository's own
// code-level rules need to inspect (artifactLanguage, displayName, envelope
// timestamps): a malformed value here is not this helper's problem to
// report — schema validation is what names that field and rejects it.
func (d Document) getString(key string) string {
	v, ok := d[key].(string)
	if !ok {
		return ""
	}
	return v
}

// asMap type-asserts v as a JSON object, accepting both map[string]any and
// this package's own Document: a caller building nested business fields
// naturally reaches for the same named type the top level uses (as this
// package's own tests do), and a plain type assertion for map[string]any
// alone would silently treat that as "not an object" instead of reading it.
func asMap(v any) (map[string]any, bool) {
	switch val := v.(type) {
	case Document:
		return val, true
	case map[string]any:
		return val, true
	default:
		return nil, false
	}
}

// plainValue recursively rewrites Document into map[string]any (and walks
// []any looking for more of it). jsonschema's validator type-switches on the
// concrete Go type of every value it walks, and store.Document — a named
// type, precisely so it can carry the clone/getString methods above — does
// not match its "map[string]any" case even though the underlying data is
// identical; a Document anywhere below the top level would otherwise fail
// validation with "invalid jsonType store.Document" regardless of its
// content. The top-level document passed to Validate is converted directly;
// this is for everything nested inside it — a caller's business fields, or
// this package's own createdBy and stateTransition-shaped values.
func plainValue(v any) any {
	switch val := v.(type) {
	case Document:
		out := make(map[string]any, len(val))
		for k, item := range val {
			out[k] = plainValue(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			out[k] = plainValue(item)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = plainValue(item)
		}
		return out
	default:
		return v
	}
}
