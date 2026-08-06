package store

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// --- splitFrontMatterBlock -------------------------------------------------

func TestSplitFrontMatterBlock(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantBlock  string
		wantBody   string
		wantReason string
	}{
		{
			name:       "missing opening delimiter",
			raw:        "title: Foo\npurpose: bar\n",
			wantReason: reasonMissingFrontMatter,
		},
		{
			name:       "opening delimiter but never closes",
			raw:        "---\ntitle: Foo\n",
			wantReason: reasonUnterminatedFrontMatter,
		},
		{
			name:       "only the opening delimiter, no trailing newline",
			raw:        "---",
			wantReason: reasonUnterminatedFrontMatter,
		},
		{
			name:      "valid, LF line endings",
			raw:       "---\ntitle: Foo\npurpose: bar\n---\nBody text.\n",
			wantBlock: "title: Foo\npurpose: bar\n",
			wantBody:  "Body text.\n",
		},
		{
			// The delimiter comparison tolerates a trailing "\r", but the
			// block's own content is returned byte-for-byte: only the
			// delimiter lines themselves are inspected, never rewritten.
			// parseFrontMatterYAML's own CRLF coverage (via the full
			// TestParseFrontMatter case below) confirms the YAML decoder
			// accepts a block with embedded "\r\n" line breaks.
			name:      "valid, CRLF line endings",
			raw:       "---\r\ntitle: Foo\r\npurpose: bar\r\n---\r\nBody text.\r\n",
			wantBlock: "title: Foo\r\npurpose: bar\r\n",
			wantBody:  "Body text.\r\n",
		},
		{
			name:      "valid, empty body",
			raw:       "---\ntitle: Foo\n---\n",
			wantBlock: "title: Foo\n",
			wantBody:  "",
		},
		{
			name:       "closing delimiter with trailing whitespace does not count",
			raw:        "---\ntitle: Foo\n---  \n",
			wantReason: reasonUnterminatedFrontMatter,
		},
		{
			name:       "opening delimiter with trailing whitespace does not count",
			raw:        "---  \ntitle: Foo\n---\n",
			wantReason: reasonMissingFrontMatter,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			block, body, reason := splitFrontMatterBlock([]byte(tt.raw))
			if reason != tt.wantReason {
				t.Fatalf("reason = %q, want %q", reason, tt.wantReason)
			}
			if tt.wantReason != "" {
				if block != nil || body != nil {
					t.Fatalf("rejection should return nil block/body, got block=%q body=%q", block, body)
				}
				return
			}
			if string(block) != tt.wantBlock {
				t.Fatalf("block = %q, want %q", block, tt.wantBlock)
			}
			if string(body) != tt.wantBody {
				t.Fatalf("body = %q, want %q", body, tt.wantBody)
			}
		})
	}
}

// TestSplitFrontMatterBlock_TolersUTF8BOM confirms the UTF-8 byte-order mark
// some editors and Windows checkouts prepend does not defeat the opening
// delimiter check.
func TestSplitFrontMatterBlock_TolersUTF8BOM(t *testing.T) {
	raw := append(append([]byte{}, bomPrefix...), []byte("---\ntitle: Foo\n---\nBody.\n")...)

	block, body, reason := splitFrontMatterBlock(raw)
	if reason != "" {
		t.Fatalf("reason = %q, want none", reason)
	}
	if string(block) != "title: Foo\n" {
		t.Fatalf("block = %q", block)
	}
	if string(body) != "Body.\n" {
		t.Fatalf("body = %q", body)
	}
}

// --- rejectUnsupportedYAML --------------------------------------------------

// TestRejectUnsupportedYAML exercises each ADR-017 restriction against a
// hand-built *yaml.Node tree, rather than through the decoder: a valid YAML
// alias always needs an anchor to point at, and that anchor's node always
// precedes the alias in document order, so decoding real YAML text can never
// isolate the alias check on its own — the anchor check fires first, simply
// because it is visited first. Building the tree directly is what lets each
// of the four restrictions be tested independently of the others.
func TestRejectUnsupportedYAML(t *testing.T) {
	strNode := func(v string) *yaml.Node {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
	}

	tests := []struct {
		name       string
		node       *yaml.Node
		wantReason string
	}{
		{
			name: "plain map, nothing to reject",
			node: &yaml.Node{
				Kind: yaml.DocumentNode,
				Content: []*yaml.Node{
					{
						Kind: yaml.MappingNode, Tag: "!!map",
						Content: []*yaml.Node{strNode("a"), strNode("1")},
					},
				},
			},
		},
		{
			name: "anchor",
			node: &yaml.Node{
				Kind: yaml.DocumentNode,
				Content: []*yaml.Node{
					{
						Kind: yaml.MappingNode, Tag: "!!map",
						Content: []*yaml.Node{
							strNode("a"),
							{Kind: yaml.ScalarNode, Tag: "!!str", Value: "1", Anchor: "x", Line: 3},
						},
					},
				},
			},
			wantReason: "anchor",
		},
		{
			name: "alias, with no anchor anywhere in the tree",
			node: &yaml.Node{
				Kind: yaml.DocumentNode,
				Content: []*yaml.Node{
					{
						Kind: yaml.MappingNode, Tag: "!!map",
						Content: []*yaml.Node{
							strNode("a"),
							{Kind: yaml.AliasNode, Value: "x", Line: 4},
						},
					},
				},
			},
			wantReason: "alias",
		},
		{
			name: "merge key",
			node: &yaml.Node{
				Kind: yaml.DocumentNode,
				Content: []*yaml.Node{
					{
						Kind: yaml.MappingNode, Tag: "!!map",
						Content: []*yaml.Node{
							{Kind: yaml.ScalarNode, Tag: "!!merge", Value: "<<", Line: 5},
							{Kind: yaml.MappingNode, Tag: "!!map"},
						},
					},
				},
			},
			wantReason: "merge key",
		},
		{
			name: "unsupported tag",
			node: &yaml.Node{
				Kind: yaml.DocumentNode,
				Content: []*yaml.Node{
					{
						Kind: yaml.MappingNode, Tag: "!!map",
						Content: []*yaml.Node{
							strNode("a"),
							{Kind: yaml.ScalarNode, Tag: "!mytag", Value: "foo", Line: 6},
						},
					},
				},
			},
			wantReason: "unsupported YAML tag",
		},
		{
			name: "unsupported tag nested inside a sequence",
			node: &yaml.Node{
				Kind: yaml.DocumentNode,
				Content: []*yaml.Node{
					{
						Kind: yaml.SequenceNode, Tag: "!!seq",
						Content: []*yaml.Node{
							{Kind: yaml.ScalarNode, Tag: "!!binary", Value: "AAAA", Line: 7},
						},
					},
				},
			},
			wantReason: "unsupported YAML tag",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason := rejectUnsupportedYAML(tt.node)
			if tt.wantReason == "" {
				if reason != "" {
					t.Fatalf("reason = %q, want none", reason)
				}
				return
			}
			if !strings.Contains(reason, tt.wantReason) {
				t.Fatalf("reason = %q, want it to contain %q", reason, tt.wantReason)
			}
		})
	}
}

// --- rejectNonStringKeys -----------------------------------------------------

func TestRejectNonStringKeys(t *testing.T) {
	tests := []struct {
		name       string
		value      any
		wantReason string
	}{
		{
			name:  "all string keys, nested",
			value: map[string]any{"a": map[string]any{"b": []any{"c", "d"}}},
		},
		{
			name:       "top-level non-string key",
			value:      map[any]any{1: "a"},
			wantReason: `non-string mapping key under "(root)"`,
		},
		{
			name:       "non-string key nested under a string-keyed map",
			value:      map[string]any{"a": map[any]any{true: "b"}},
			wantReason: `non-string mapping key under "a"`,
		},
		{
			name:       "non-string key nested inside a list",
			value:      map[string]any{"a": []any{map[any]any{1: "b"}}},
			wantReason: `non-string mapping key under "a/0"`,
		},
		{
			name:  "not a map at all",
			value: []any{"a", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason := rejectNonStringKeys(tt.value, "")
			if tt.wantReason == "" {
				if reason != "" {
					t.Fatalf("reason = %q, want none", reason)
				}
				return
			}
			if !strings.Contains(reason, tt.wantReason) {
				t.Fatalf("reason = %q, want it to contain %q", reason, tt.wantReason)
			}
		})
	}
}

// --- parseFrontMatterYAML ----------------------------------------------------

func TestParseFrontMatterYAML(t *testing.T) {
	tests := []struct {
		name       string
		block      string
		wantDoc    Document
		wantReason string
	}{
		{
			name:    "valid minimal",
			block:   "title: Foo\npurpose: bar\nlanguage: en\n",
			wantDoc: Document{"title": "Foo", "purpose": "bar", "language": "en"},
		},
		{
			name:    "empty block decodes as an empty document, not a parse error",
			block:   "",
			wantDoc: Document{},
		},
		{
			name:    "whitespace-and-comments-only block also decodes empty",
			block:   "  \n# just a comment\n",
			wantDoc: Document{},
		},
		{
			name:       "malformed YAML",
			block:      "title: [unterminated\n",
			wantReason: "not valid YAML",
		},
		{
			name:       "two documents",
			block:      "title: Foo\n---\ntitle: Bar\n",
			wantReason: "more than one YAML document",
		},
		{
			name:       "anchor and alias together",
			block:      "base: &x Foo\ntitle: *x\n",
			wantReason: "ADR-017 forbids",
		},
		{
			name:       "merge key without any alias",
			block:      "base:\n  x: 1\ntitle:\n  <<: {x: 1}\n  y: 2\n",
			wantReason: "merge key",
		},
		{
			name:       "custom tag",
			block:      "title: !mytag Foo\n",
			wantReason: "unsupported YAML tag",
		},
		{
			name:       "non-string mapping key",
			block:      "1: a\n",
			wantReason: "non-string mapping key",
		},
		{
			name:       "top-level sequence, not a mapping",
			block:      "- a\n- b\n",
			wantReason: "must be a YAML mapping",
		},
		{
			name:       "top-level scalar, not a mapping",
			block:      "just a string\n",
			wantReason: "must be a YAML mapping",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, reason := parseFrontMatterYAML([]byte(tt.block))
			if tt.wantReason == "" {
				if reason != "" {
					t.Fatalf("reason = %q, want none", reason)
				}
				if len(doc) != len(tt.wantDoc) {
					t.Fatalf("doc = %#v, want %#v", doc, tt.wantDoc)
				}
				for k, v := range tt.wantDoc {
					if doc[k] != v {
						t.Fatalf("doc[%q] = %#v, want %#v", k, doc[k], v)
					}
				}
				return
			}
			if reason == "" {
				t.Fatalf("parseFrontMatterYAML succeeded, want rejection containing %q", tt.wantReason)
			}
			if !strings.Contains(reason, tt.wantReason) {
				t.Fatalf("reason = %q, want it to contain %q", reason, tt.wantReason)
			}
			if doc != nil {
				t.Fatalf("doc = %#v, want nil on rejection", doc)
			}
		})
	}
}

// --- parseFrontMatter (whole file: split + parse) ----------------------------

func TestParseFrontMatter(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantDoc    Document
		wantBody   string
		wantReason string
	}{
		{
			name:     "valid, LF",
			raw:      "---\ntitle: Foo\npurpose: bar\nlanguage: en\n---\nThe body.\n",
			wantDoc:  Document{"title": "Foo", "purpose": "bar", "language": "en"},
			wantBody: "The body.\n",
		},
		{
			name:     "valid, CRLF throughout",
			raw:      "---\r\ntitle: Foo\r\npurpose: bar\r\nlanguage: en\r\n---\r\nThe body.\r\n",
			wantDoc:  Document{"title": "Foo", "purpose": "bar", "language": "en"},
			wantBody: "The body.\r\n",
		},
		{
			name:       "missing opening delimiter",
			raw:        "title: Foo\n",
			wantReason: reasonMissingFrontMatter,
		},
		{
			name:       "unterminated",
			raw:        "---\ntitle: Foo\n",
			wantReason: reasonUnterminatedFrontMatter,
		},
		{
			name:       "custom tag",
			raw:        "---\ntitle: !mytag Foo\n---\nBody.\n",
			wantReason: "unsupported YAML tag",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, body, reason := parseFrontMatter([]byte(tt.raw))
			if tt.wantReason == "" {
				if reason != "" {
					t.Fatalf("reason = %q, want none", reason)
				}
				for k, v := range tt.wantDoc {
					if doc[k] != v {
						t.Fatalf("doc[%q] = %#v, want %#v", k, doc[k], v)
					}
				}
				if string(body) != tt.wantBody {
					t.Fatalf("body = %q, want %q", body, tt.wantBody)
				}
				return
			}
			if !strings.Contains(reason, tt.wantReason) {
				t.Fatalf("reason = %q, want it to contain %q", reason, tt.wantReason)
			}
			if doc != nil || body != nil {
				t.Fatalf("rejection should return nil doc/body")
			}
		})
	}
}

// TestParseFrontMatter_SecondDocumentReachableViaInlineDashes documents a
// finding worth pinning with a test: ADR-017's "no multiple documents"
// restriction is not merely defensive. splitFrontMatterBlock's own closing
// delimiter must be a line that is *exactly* "---", but YAML's document
// separator only has to *start* a line with "---" — "--- {a: 1}" is valid
// YAML for "start a new document whose content is {a: 1}, all on this
// line". Such a line fails isDelimiterLine (it is not exactly "---"), so it
// stays inside the extracted block instead of closing it, and the decoder
// then reads it as a genuine second document. So a real markdown file can
// reach parseFrontMatterYAML's two-document rejection end to end, not only
// through a hand-fed block — confirmed here rather than assumed.
func TestParseFrontMatter_SecondDocumentReachableViaInlineDashes(t *testing.T) {
	raw := "---\ntitle: Foo\n--- {purpose: smuggled}\n---\nBody.\n"

	doc, body, reason := parseFrontMatter([]byte(raw))
	if reason == "" {
		t.Fatalf("parseFrontMatter succeeded with doc=%#v body=%q, want a rejection", doc, body)
	}
	if !strings.Contains(reason, "more than one YAML document") {
		t.Fatalf("reason = %q, want it to mention multiple documents", reason)
	}
}

// --- schema-level integration (parseFrontMatter + Repository.validate) ------

const (
	testDocumentID  = "01J8Z3K2M4N5P6Q7R8S9T0V1W2"
	testDocumentID2 = "01J8Z3K2M4N5P6Q7R8S9T0V1W3"
)

func validateFrontMatterFile(t *testing.T, repo *Repository, raw string) error {
	t.Helper()

	doc, _, reason := parseFrontMatter([]byte(raw))
	if reason != "" {
		t.Fatalf("parseFrontMatter rejected a file meant to reach schema validation: %s", reason)
	}
	return repo.validate("document-front-matter.schema.json", "docs/test.md", doc)
}

func TestParseFrontMatter_SchemaValidation(t *testing.T) {
	repo := newTestRepository(t)

	t.Run("full valid document", func(t *testing.T) {
		raw := "---\n" +
			"schemaVersion: 1\n" +
			"id: " + testDocumentID + "\n" +
			"title: Provider adapter contract\n" +
			"purpose: Describes the five responsibilities every adapter implements.\n" +
			"language: en\n" +
			"tags: [documentation, providers]\n" +
			"relations:\n" +
			"  - kind: implements\n" +
			"    target: {type: decision, id: " + testDocumentID2 + "}\n" +
			"---\n" +
			"Body.\n"
		if err := validateFrontMatterFile(t, repo, raw); err != nil {
			t.Fatalf("validate: %v", err)
		}
	})

	t.Run("required field absent", func(t *testing.T) {
		raw := "---\n" +
			"schemaVersion: 1\n" +
			"id: " + testDocumentID + "\n" +
			"title: Provider adapter contract\n" +
			"language: en\n" +
			"---\n" +
			"Body.\n"
		err := validateFrontMatterFile(t, repo, raw)
		requireRejected(t, err, "purpose")
	})

	t.Run("unknown relation kind", func(t *testing.T) {
		raw := "---\n" +
			"schemaVersion: 1\n" +
			"id: " + testDocumentID + "\n" +
			"title: Provider adapter contract\n" +
			"purpose: Describes responsibilities.\n" +
			"language: en\n" +
			"relations:\n" +
			"  - kind: contradicts\n" +
			"    target: {type: decision, id: " + testDocumentID2 + "}\n" +
			"---\n" +
			"Body.\n"
		err := validateFrontMatterFile(t, repo, raw)
		requireRejected(t, err, "relations/0/kind")
	})

	t.Run("unknown property", func(t *testing.T) {
		raw := "---\n" +
			"schemaVersion: 1\n" +
			"id: " + testDocumentID + "\n" +
			"title: Provider adapter contract\n" +
			"purpose: Describes responsibilities.\n" +
			"language: en\n" +
			"summary: not a real field\n" +
			"---\n" +
			"Body.\n"
		err := validateFrontMatterFile(t, repo, raw)
		requireRejected(t, err, "summary")
	})

	t.Run("language as a boolean is rejected", func(t *testing.T) {
		raw := "---\n" +
			"schemaVersion: 1\n" +
			"id: " + testDocumentID + "\n" +
			"title: Provider adapter contract\n" +
			"purpose: Describes responsibilities.\n" +
			"language: true\n" +
			"---\n" +
			"Body.\n"
		err := validateFrontMatterFile(t, repo, raw)
		requireRejected(t, err, "language")
	})

	t.Run("language as the literal 'no' is accepted as the string, not a boolean", func(t *testing.T) {
		// This is ADR-017's own "Norwegian problem" example, and it is safe
		// with this library for the reason documented on parseFrontMatterYAML:
		// go.yaml.in/yaml/v3 is YAML 1.2, which does not treat `no` as a
		// boolean literal at all, so it decodes as the string "no" — which
		// also happens to be the correct ISO 639-1 code for Norwegian.
		raw := "---\n" +
			"schemaVersion: 1\n" +
			"id: " + testDocumentID + "\n" +
			"title: Provider adapter contract\n" +
			"purpose: Describes responsibilities.\n" +
			"language: no\n" +
			"---\n" +
			"Body.\n"
		doc, _, reason := parseFrontMatter([]byte(raw))
		if reason != "" {
			t.Fatalf("parseFrontMatter: %s", reason)
		}
		if lang, ok := doc["language"].(string); !ok || lang != "no" {
			t.Fatalf("doc[\"language\"] = %#v (%T), want the string \"no\"", doc["language"], doc["language"])
		}
		if err := repo.validate("document-front-matter.schema.json", "docs/test.md", doc); err != nil {
			t.Fatalf("validate: %v", err)
		}
	})
}
