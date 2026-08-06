package store

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"

	"go.yaml.in/yaml/v3"
)

// ADR-017 restricts a document's front matter to a plain subset of YAML:
// between --- delimiters, no anchors, no aliases, no custom tags, no
// multi-document streams. This file is the parser that imposes that subset —
// none of the four restrictions holds on its own against go.yaml.in/yaml/v3
// (measured, not assumed, before writing this):
//
//   - yaml.Unmarshal into a plain value silently accepts a second document in
//     the stream, decoding only the first and dropping the rest without
//     error.
//   - Anchors and aliases resolve without error; so does the merge key (<<),
//     which is YAML 1.1's mechanism for aliased-map inheritance and needs no
//     alias of its own to appear (`<<: {x: 1}` merges without ever using *).
//   - Custom tags (`!whatever foo`) resolve to a scalar without complaint.
//
// So every one of ADR-017's four restrictions is enforced explicitly below,
// not inherited from the library's defaults.
//
// ADR-017 illustrates its decision with the "Norwegian problem": a bare
// `language: no` that a YAML 1.1 parser turns into the boolean false, which
// then has to be caught by schema validation instead of surfacing as the
// string a human typed. That specific mechanism does not fire with this
// library: go.yaml.in/yaml/v3 implements YAML 1.2 core schema resolution,
// where `no` is not a boolean literal, so `language: no` decodes as the
// string "no" — which happens to also be the correct ISO 639-1 code for
// Norwegian (schemas/document-front-matter.schema.json requires
// languageCode). The ADR's decision — validate the parsed result, don't pick
// a format to dodge the ambiguity — still stands, and is still what makes
// the platform safe against the class of bug: `language: true` is a real
// YAML 1.2 boolean and document-front-matter.schema.json rejects it exactly
// as the ADR describes. What does not happen with this library is the
// specific conversion the ADR's example walks through. This is noted here,
// not in the ADR, because it is an implementation detail of the library this
// task chose, not a change to the decision.

// reasonMissingFrontMatter and reasonUnterminatedFrontMatter are the two
// distinct rejections splitFrontMatterBlock can report before any YAML
// parsing is attempted at all. They are kept as different reasons — not
// folded into one "no front matter" message — because they point a human at
// different fixes: add an opening line, or add the missing closing one.
const (
	reasonMissingFrontMatter      = `missing front matter: the file must start with a line that is exactly "---"`
	reasonUnterminatedFrontMatter = `unterminated front matter: no closing "---" line was found`
)

// bomPrefix is the UTF-8 byte order mark some editors and, notably, Windows
// checkout tooling prepend to a file. It is stripped before the delimiter
// scan below, so a file's very first three bytes being the BOM does not make
// splitFrontMatterBlock miss the opening "---" line that follows it.
var bomPrefix = []byte{0xEF, 0xBB, 0xBF}

// splitFrontMatterBlock locates the front-matter block ADR-017 describes
// within raw: a line that is exactly "---", followed by the YAML text,
// followed by another line that is exactly "---". block is everything
// between the two delimiter lines (with neither delimiter itself); body is
// everything after the closing delimiter's line terminator, which is the
// markdown a reader — and document_fts — sees as the document's content.
//
// A trailing "\r" is trimmed before either delimiter comparison, and a UTF-8
// BOM is stripped before the scan starts: CI's Windows runner checks this
// repository out with CRLF line endings and a BOM is a real possibility from
// editors on that platform, so both are tolerated rather than hypothetical.
//
// reason is one of reasonMissingFrontMatter or reasonUnterminatedFrontMatter
// when the block cannot be located at all; block and body are nil in that
// case, and the caller turns reason into a document_issue rather than
// attempting to parse anything.
func splitFrontMatterBlock(raw []byte) (block, body []byte, reason string) {
	content := bytes.TrimPrefix(raw, bomPrefix)

	first, rest, _ := cutLine(content)
	if !isDelimiterLine(first) {
		return nil, nil, reasonMissingFrontMatter
	}

	var buf bytes.Buffer
	remaining := rest
	for {
		line, after, ok := cutLine(remaining)
		if isDelimiterLine(line) {
			return buf.Bytes(), after, ""
		}
		if !ok {
			// remaining had no more line terminators: line is the last,
			// unterminated line in the file, and it is not the closer.
			return nil, nil, reasonUnterminatedFrontMatter
		}
		buf.Write(line)
		buf.WriteByte('\n')
		remaining = after
	}
}

// cutLine splits content at its first '\n', returning the line before it
// (which may still carry a trailing '\r', deliberately left for
// isDelimiterLine to trim) and everything after the '\n'. ok is false when
// content has no '\n' at all, in which case line is all of content — the
// file's final, unterminated line.
func cutLine(content []byte) (line, after []byte, ok bool) {
	i := bytes.IndexByte(content, '\n')
	if i < 0 {
		return content, nil, false
	}
	return content[:i], content[i+1:], true
}

// isDelimiterLine reports whether line is exactly "---", once a trailing
// "\r" (CRLF) is trimmed. Anything else on the line — trailing whitespace,
// inline YAML content, a comment — disqualifies it: ADR-017 asks for a line
// that is exactly the delimiter, not a line that merely starts with one.
func isDelimiterLine(line []byte) bool {
	return string(bytes.TrimSuffix(line, []byte("\r"))) == "---"
}

// standardYAMLTags is the resolved-tag whitelist ADR-017's plain subset
// allows: the seven YAML core types a JSON-shaped document can need. Every
// other tag — a custom one, or a YAML 1.1 extension like !!merge or
// !!binary — is rejected by rejectUnsupportedYAML below.
var standardYAMLTags = map[string]bool{
	"!!map":   true,
	"!!seq":   true,
	"!!str":   true,
	"!!int":   true,
	"!!float": true,
	"!!bool":  true,
	"!!null":  true,
}

// rejectUnsupportedYAML walks node's tree and names the first construct
// ADR-017's plain subset forbids: an anchor, an alias, a merge key, or a tag
// outside standardYAMLTags. It returns "" when the whole tree is plain.
//
// The merge-key check runs before the generic tag check reaches the same
// node, and is deliberately separate from the alias rejection: a merge can
// appear without any alias at all (`<<: {x: 1}` merges an inline mapping,
// never dereferencing *anything*), so the alias check alone would miss it.
// The tag check would also catch it in isolation — a merge key's resolved
// tag is !!merge, which is not in standardYAMLTags — but naming it as a
// merge key explicitly produces a more useful reason than "tag !!merge is
// not supported".
func rejectUnsupportedYAML(node *yaml.Node) string {
	if node.Anchor != "" {
		return fmt.Sprintf("front matter uses a YAML anchor (%q) at line %d, which ADR-017 forbids", node.Anchor, node.Line)
	}
	if node.Kind == yaml.AliasNode {
		return fmt.Sprintf("front matter uses a YAML alias at line %d, which ADR-017 forbids", node.Line)
	}

	if node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			if key := node.Content[i]; key.Value == "<<" {
				return fmt.Sprintf(`front matter uses a YAML merge key ("<<") at line %d, which ADR-017 forbids`, key.Line)
			}
		}
	}

	if node.Kind != yaml.DocumentNode && !standardYAMLTags[node.Tag] {
		return fmt.Sprintf("front matter uses an unsupported YAML tag %q at line %d, which ADR-017 forbids", node.Tag, node.Line)
	}

	for _, child := range node.Content {
		if reason := rejectUnsupportedYAML(child); reason != "" {
			return reason
		}
	}
	return ""
}

// rejectNonStringKeys walks a value already decoded by (*yaml.Node).Decode
// into `any` and names the first map with a non-string key. yaml.v3 decodes
// a mapping into map[string]any only when every key in it is a string;
// mixing in a single non-string key (an integer, a bool, a nested map used
// as a key) makes it decode that mapping as map[any]any instead — measured,
// not assumed. The jsonschema validator this package uses already fails
// closed on that type with "invalid jsonType" (the same property
// TestUnrecognizedGoValue_FailsValidationLoudly pins in document_test.go for
// T-020), but an explicit, named rejection here repairs better than that
// message does, so this check runs before validation is ever attempted.
//
// path accumulates a "/"-joined location as the walk descends, the same
// convention joinField uses for JSON Schema field paths (validate.go); map
// keys are visited in sorted order so the reported location does not depend
// on Go's randomized map iteration — this parser has to be as deterministic
// as the indexer built on it.
func rejectNonStringKeys(value any, path string) string {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if reason := rejectNonStringKeys(v[k], joinField(path, k)); reason != "" {
				return reason
			}
		}
		return ""
	case map[any]any:
		location := path
		if location == "" {
			location = "(root)"
		}
		return fmt.Sprintf("front matter has a non-string mapping key under %q", location)
	case []any:
		for i, item := range v {
			if reason := rejectNonStringKeys(item, fmt.Sprintf("%s/%d", path, i)); reason != "" {
				return reason
			}
		}
		return ""
	default:
		return ""
	}
}

// parseFrontMatterYAML parses block — the bytes between the two "---"
// delimiters, as returned by splitFrontMatterBlock — under ADR-017's plain
// subset: exactly one YAML document, no anchors, aliases, custom tags or
// merge keys, and no non-string mapping keys anywhere in it.
//
// A non-empty reason means block was rejected before schema validation ever
// saw it; doc is nil in that case. Structural validation against
// document-front-matter.schema.json happens one level up, in
// documentindex.go, against the Document this returns — this function only
// answers "is this YAML the plain subset ADR-017 allows", not "is this a
// well-formed document".
func parseFrontMatterYAML(block []byte) (doc Document, reason string) {
	dec := yaml.NewDecoder(bytes.NewReader(block))

	var node yaml.Node
	if err := dec.Decode(&node); err != nil {
		if errors.Is(err, io.EOF) {
			// A block with nothing in it (blank, or only comments) is zero
			// YAML documents, not a parse error. Reporting "not valid YAML"
			// here would be misleading: there is nothing malformed, just
			// nothing present. Returning an empty Document lets schema
			// validation name every required field this document is
			// missing (title, purpose, language) by the same repairable-error
			// path every other rejection goes through, instead of this
			// function inventing a second, parallel way to say "empty".
			return Document{}, ""
		}
		return nil, fmt.Sprintf("front matter is not valid YAML: %v", err)
	}

	var second yaml.Node
	if err := dec.Decode(&second); err == nil {
		return nil, "front matter contains more than one YAML document, which ADR-017 forbids"
	} else if !errors.Is(err, io.EOF) {
		return nil, fmt.Sprintf("front matter is not valid YAML: %v", err)
	}

	if reason := rejectUnsupportedYAML(&node); reason != "" {
		return nil, reason
	}

	var value any
	if err := node.Decode(&value); err != nil {
		return nil, fmt.Sprintf("front matter is not valid YAML: %v", err)
	}

	if reason := rejectNonStringKeys(value, ""); reason != "" {
		return nil, reason
	}

	top, ok := value.(map[string]any)
	if !ok {
		return nil, "front matter must be a YAML mapping, not a list or a bare scalar"
	}
	return Document(top), ""
}

// parseFrontMatter splits raw — a whole markdown file's bytes — into its
// front matter and body, and parses the front matter under ADR-017's plain
// subset. A non-empty reason means the document is not indexable; doc and
// body are nil in that case, and documentindex.go turns reason into a
// document_issue rather than an index entry — a front-matter block that
// fails to parse and one that parses but fails schema validation are
// indistinguishable to the person who has to fix them, and 03-arquitectura.md:85
// requires both be marked, never silently dropped.
//
// On success, doc is not yet known to be schema-valid: this function only
// answers "is this the plain YAML subset ADR-017 allows", not "is this a
// well-formed document-front-matter". Schema validation is documentindex.go's
// job, against the compiled schema a *Repository already holds.
func parseFrontMatter(raw []byte) (doc Document, body []byte, reason string) {
	block, rest, reason := splitFrontMatterBlock(raw)
	if reason != "" {
		return nil, nil, reason
	}

	doc, reason = parseFrontMatterYAML(block)
	if reason != "" {
		return nil, nil, reason
	}
	return doc, rest, ""
}
