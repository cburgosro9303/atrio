package schemas_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"

	"github.com/cburgosro9303/atrio/schemas"
)

const (
	draft2020 = "https://json-schema.org/draft/2020-12/schema"

	// The definitions file every artifact composes with. It declares no artifact
	// of its own, so the conventions that close an artifact do not apply to it.
	commonSchema = "common.schema.json"

	// The envelope every JSON artifact carries: schemaVersion, id, timestamps and
	// attribution (ADR-014).
	envelopeRef = commonSchema + "#/$defs/envelope"

	// Front-matter carries the reduced form instead: a hand-edited markdown file
	// has no business restating what git already records.
	documentEnvelopeRef = commonSchema + "#/$defs/documentEnvelope"

	// The marketplace manifest carries a third form. It is not an artifact of a
	// project: the official definitions repository publishes it per tag, so it has
	// neither a ULID of its own nor a git identity to attribute it to (T-011).
	manifestEnvelopeRef = commonSchema + "#/$defs/manifestEnvelope"

	// A canonical definition of the catalog carries a fourth. Like the manifest it
	// is published rather than created in a project, and unlike the manifest it
	// carries no version of its own: the tag that ships it is its version (T-012).
	definitionEnvelopeRef = commonSchema + "#/$defs/definitionEnvelope"

	// An invalid fixture is named <property>--<reason>.json. The property is the
	// one whose rule the fixture breaks, and the test demands that the validator
	// point at it: a rejection that cannot say which field is wrong is not the
	// repairable error the platform promises.
	reasonSeparator = "--"
)

// envelopeFamilies names the schemas whose envelope is not the artifact one.
// Everything absent from here is an artifact of a project and carries the full
// envelope, which is what keeps the default failing closed: a schema added
// without an entry is held to the artifact rule until someone decides otherwise.
var envelopeFamilies = map[string]string{
	"document-front-matter.schema.json": documentEnvelopeRef,
	"marketplace-manifest.schema.json":  manifestEnvelopeRef,
	"agent-definition.schema.json":      definitionEnvelopeRef,
	"flow-definition.schema.json":       definitionEnvelopeRef,
}

// TestEverySchemaCompiles is the floor the other rules stand on. Compiling
// validates each schema against the 2020-12 meta-schema and resolves every $ref,
// so a typo in a keyword or a reference to a definition that does not exist stops
// here rather than turning into a schema that accepts everything.
func TestEverySchemaCompiles(t *testing.T) {
	for _, name := range schemaFiles(t) {
		t.Run(name, func(t *testing.T) {
			compile(t, name)
		})
	}
}

// TestSchemaConventions makes the conventions of T-010 executable. They are the
// kind of decision that a reader of one schema copies into the next, so leaving
// them to prose guarantees drift by the third file.
func TestSchemaConventions(t *testing.T) {
	for _, name := range schemaFiles(t) {
		t.Run(name, func(t *testing.T) {
			document := decodeSchema(t, name)

			if got := document["$schema"]; got != draft2020 {
				t.Errorf("$schema is %v, want %s — the envelope is composed with allOf and "+
					"closed with unevaluatedProperties, which only 2020-12 provides", got, draft2020)
			}
			if got := document["$id"]; got != name {
				t.Errorf("$id is %v, want %q: the relative id has to match the file name for a "+
					"$ref between schemas to resolve without a base URI", got, name)
			}
			for _, field := range []string{"title", "description"} {
				if text, ok := document[field].(string); !ok || strings.TrimSpace(text) == "" {
					t.Errorf("%s is empty; the schemas are the published contract and this is "+
						"the only documentation that travels with them", field)
				}
			}

			if name == commonSchema {
				return
			}

			if got, ok := document["unevaluatedProperties"].(bool); !ok || got {
				t.Errorf("unevaluatedProperties is %v, want false: an unknown field is a typo "+
					"in a field that matters, and it must be rejected rather than ignored", document["unevaluatedProperties"])
			}
			// Exactly one envelope, and the one this schema's family calls for.
			// Merely carrying *an* envelope would let a document declare the
			// artifact's — inventing a ULID and an author for a file that has
			// neither — and let the manifest pass while claiming to be an artifact.
			want := expectedEnvelope(name)
			switch carried := envelopeRefs(document); {
			case len(carried) == 0:
				t.Errorf("no allOf entry references an envelope; every schema carries exactly "+
					"one (ADR-014), and this one's is %s", want)
			case len(carried) > 1:
				t.Errorf("references %d envelopes (%v); a schema carries exactly one, or the "+
					"fields of the other arrive without anybody having decided they should", len(carried), carried)
			case carried[0] != want:
				t.Errorf("carries %s, want %s: the envelope is what says which family a "+
					"schema belongs to, and this one belongs to the other", carried[0], want)
			}
		})
	}
}

// TestFixturesMatchTheirSchema exercises every schema against its fixtures. The
// valid ones must be accepted; the invalid ones must be rejected *and* name the
// field at fault.
func TestFixturesMatchTheirSchema(t *testing.T) {
	for _, name := range schemaFiles(t) {
		if name == commonSchema {
			continue
		}

		t.Run(name, func(t *testing.T) {
			schema := compile(t, name)

			for _, fixture := range fixtures(t, name, "valid") {
				t.Run("valid/"+filepath.Base(fixture), func(t *testing.T) {
					if err := schema.Validate(decodeFixture(t, fixture)); err != nil {
						t.Errorf("a valid artifact was rejected:\n%v", err)
					}
				})
			}

			for _, fixture := range fixtures(t, name, "invalid") {
				base := filepath.Base(fixture)
				t.Run("invalid/"+base, func(t *testing.T) {
					property, ok := brokenProperty(base)
					if !ok {
						t.Fatalf("fixture %s does not follow <property>%s<reason>.json, so the "+
							"test cannot tell which field it breaks", base, reasonSeparator)
					}

					err := schema.Validate(decodeFixture(t, fixture))
					if err == nil {
						t.Fatalf("an invalid artifact was accepted; the rule about %q is not "+
							"being enforced", property)
					}

					if !locatesProperty(err, property) {
						t.Errorf("the rejection never points at %q, so it cannot tell the author "+
							"what to repair:\n%v", property, err)
					}
				})
			}
		})
	}
}

// TestPermissionCategoriesMatchTheCatalog keeps the seven categories from
// drifting apart. agents.json declares a level for each one as a property name;
// the log names the one an authorization was about as an enum value. They are
// two spellings of the same closed catalog, and a category added to one and not
// the other would quietly lose either its level or its audit trail.
func TestPermissionCategoriesMatchTheCatalog(t *testing.T) {
	catalog := definition(t, decodeSchema(t, commonSchema), "$defs", "permissionCategory")
	categories := names(t, catalog, "enum")

	agents := decodeSchema(t, "agents.schema.json")
	permissionMap := definition(t, agents, "$defs", "permissionMap")

	levels := definition(t, permissionMap, "properties")
	mapped := make([]string, 0, len(levels))
	for name := range levels {
		mapped = append(mapped, name)
	}

	// The map has to demand every category, not merely allow it: a declared map
	// that leaves one out leaves that category undecided, which is the hole the
	// permission model exists to close.
	demanded := names(t, permissionMap, "required")

	slices.Sort(categories)
	slices.Sort(mapped)
	slices.Sort(demanded)

	if !slices.Equal(categories, mapped) {
		t.Errorf("the permission catalogs drifted apart\n%s: %v\nagents.schema.json properties: %v",
			commonSchema, categories, mapped)
	}
	if !slices.Equal(categories, demanded) {
		t.Errorf("the map does not demand every category\ncatalog:  %v\nrequired: %v",
			categories, demanded)
	}
}

// TestCatalogRefBuildsOnCatalogId keeps the two spellings of a catalog
// identifier from drifting apart. The marketplace manifest names an item by its
// id alone — the version of every item is the manifest's own platformVersion —
// and a project declares that same item as id@version. If the id pattern came to
// admit a character catalogRef's does not, the manifest could ship a perfectly
// legal item that no project is able to reference.
func TestCatalogRefBuildsOnCatalogId(t *testing.T) {
	common := decodeSchema(t, commonSchema)
	id := text(t, definition(t, common, "$defs", "catalogId"), "pattern")
	ref := text(t, definition(t, common, "$defs", "catalogRef"), "pattern")

	body, anchored := strings.CutSuffix(id, "$")
	if !anchored {
		t.Fatalf("catalogId's pattern %q is not anchored at the end, so it constrains a prefix "+
			"of an identifier rather than the whole of one", id)
	}

	if !strings.HasPrefix(ref, body+"@") {
		t.Errorf("catalogRef no longer builds on catalogId\ncatalogId:  %s\ncatalogRef: %s\n"+
			"want catalogRef to begin with %q", id, ref, body+"@")
	}
}

// TestKeyConstraintsCanNameTheirField bans propertyNames outright. Constraining
// the keys of a map with it is the obvious thing to reach for and it silently
// breaks the promise this whole suite exists to keep: the validator reports a
// propertyNames failure with an *empty* instance location — verified against
// v6.0.2, where every level of the cause tree came back empty — so the rejection
// cannot say which field the bad key was in. patternProperties with
// additionalProperties: false enforces exactly the same thing and fails at the
// map's own location, which is what makes the error repairable.
func TestKeyConstraintsCanNameTheirField(t *testing.T) {
	for _, name := range schemaFiles(t) {
		t.Run(name, func(t *testing.T) {
			if where, found := findKeyword(decodeSchema(t, name), "propertyNames", ""); found {
				t.Errorf("propertyNames at %s: its rejection carries no instance location, so it "+
					"cannot name the field at fault. Use patternProperties with "+
					"additionalProperties: false, which constrains the same keys and fails where "+
					"the author can see it", where)
			}
		})
	}
}

// keyPatternsWithoutADefinition names the patternProperties keys that mirror
// nothing in common, and why. A key map whose namespace is genuinely its own
// belongs here rather than forcing a definition into common that no other schema
// would ever reference — but it belongs here *named*, so that the rule below can
// fail closed on every other site.
var keyPatternsWithoutADefinition = map[string]string{
	"project-config.schema.json/properties/schemaVersions/patternProperties": "the keys are the document types that carry a schema version, deliberately a " +
		"broader namespace than the five values of artifactType",
}

// TestPatternKeysMatchTheirDefinition covers the cost of the rule above. A
// patternProperties key is a literal regular expression, so it cannot $ref the
// definition it mirrors and the pattern ends up written out a second time. The
// rule sweeps the whole corpus rather than a hand-kept list of sites: a list
// would leave the next map anybody adds silently unchecked, which is the drift
// this project synchronizes with a test instead of a comment.
func TestPatternKeysMatchTheirDefinition(t *testing.T) {
	declared := declaredPatterns(t)

	var swept int
	for _, name := range schemaFiles(t) {
		for _, site := range patternKeys(decodeSchema(t, name), name) {
			swept++
			t.Run(site.where, func(t *testing.T) {
				if _, deliberate := keyPatternsWithoutADefinition[site.where]; deliberate {
					return
				}
				definedAs, ok := declared[site.pattern]
				if !ok {
					t.Errorf("the key pattern %s matches no definition in %s, so this copy has "+
						"nothing keeping it honest. Either mirror a definition or say in "+
						"keyPatternsWithoutADefinition why this namespace is its own", site.pattern, commonSchema)
					return
				}
				// Several definitions share one pattern — a catalog id, a role, a
				// stage and a provider all spell an identifier the same way — so the
				// note lists every one of them rather than picking whichever the map
				// happened to yield.
				t.Logf("mirrors %s", strings.Join(definedAs, ", "))
			})
		}
	}

	if swept == 0 {
		t.Fatal("no patternProperties key found anywhere; the sweep is comparing nothing against nothing")
	}

	// An exemption that outlives the map it was written for is an exemption that
	// quietly widens the rule.
	for where := range keyPatternsWithoutADefinition {
		if !slices.ContainsFunc(allPatternKeys(t), func(site patternKeySite) bool { return site.where == where }) {
			t.Errorf("keyPatternsWithoutADefinition still exempts %s, which no longer exists", where)
		}
	}
}

type patternKeySite struct {
	where   string
	pattern string
}

func allPatternKeys(t *testing.T) []patternKeySite {
	t.Helper()

	var found []patternKeySite
	for _, name := range schemaFiles(t) {
		found = append(found, patternKeys(decodeSchema(t, name), name)...)
	}
	return found
}

// patternKeys collects every patternProperties key in a schema, wherever it sits.
func patternKeys(node any, path string) []patternKeySite {
	var found []patternKeySite

	switch value := node.(type) {
	case map[string]any:
		for key, child := range value {
			where := path + "/" + key
			if key == "patternProperties" {
				if keys, ok := child.(map[string]any); ok {
					for pattern := range keys {
						found = append(found, patternKeySite{where: where, pattern: pattern})
					}
				}
				continue
			}
			found = append(found, patternKeys(child, where)...)
		}
	case []any:
		for i, child := range value {
			found = append(found, patternKeys(child, fmt.Sprintf("%s/%d", path, i))...)
		}
	}

	slices.SortFunc(found, func(a, b patternKeySite) int { return strings.Compare(a.where, b.where) })
	return found
}

// declaredPatterns indexes the patterns common declares, so a copy of one can be
// recognized as a copy.
func declaredPatterns(t *testing.T) map[string][]string {
	t.Helper()

	patterns := make(map[string][]string)
	for name, node := range definition(t, decodeSchema(t, commonSchema), "$defs") {
		if entry, ok := node.(map[string]any); ok {
			if pattern, ok := entry["pattern"].(string); ok {
				patterns[pattern] = append(patterns[pattern], name)
			}
		}
	}
	for _, names := range patterns {
		slices.Sort(names)
	}
	if len(patterns) == 0 {
		t.Fatalf("%s declares no pattern at all", commonSchema)
	}
	return patterns
}

// TestEveryArtifactSchemaHasFixtures fails closed on a schema added without
// tests. Without it, the suite above would report a serene green over a schema
// nobody ever exercised.
func TestEveryArtifactSchemaHasFixtures(t *testing.T) {
	for _, name := range schemaFiles(t) {
		if name == commonSchema {
			continue
		}

		t.Run(name, func(t *testing.T) {
			for _, kind := range []string{"valid", "invalid"} {
				if found := fixtures(t, name, kind); len(found) == 0 {
					t.Errorf("no %s fixtures under %s", kind, fixtureDir(name, kind))
				}
			}
		})
	}
}

// schemaFiles lists the embedded schemas. It fails when the list is empty: a
// glob that stops matching would otherwise turn every rule above into a loop
// over nothing.
func schemaFiles(t *testing.T) []string {
	t.Helper()

	names, err := fs.Glob(schemas.FS, "*.schema.json")
	if err != nil {
		t.Fatalf("listing the embedded schemas: %v", err)
	}
	if len(names) == 0 {
		t.Fatal("no embedded schemas found; check the go:embed pattern")
	}
	return names
}

// compile resolves one schema with every other schema available, so relative
// references between them behave as they will in production.
func compile(t *testing.T, name string) *jsonschema.Schema {
	t.Helper()

	compiler := jsonschema.NewCompiler()
	for _, other := range schemaFiles(t) {
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(readSchema(t, other)))
		if err != nil {
			t.Fatalf("%s is not valid JSON: %v", other, err)
		}
		if err := compiler.AddResource(other, document); err != nil {
			t.Fatalf("registering %s: %v", other, err)
		}
	}

	schema, err := compiler.Compile(name)
	if err != nil {
		t.Fatalf("compiling %s: %v", name, err)
	}
	return schema
}

func readSchema(t *testing.T, name string) []byte {
	t.Helper()

	content, err := schemas.FS.ReadFile(name)
	if err != nil {
		t.Fatalf("reading the embedded %s: %v", name, err)
	}
	return content
}

// decodeSchema reads a schema as plain data, which is how the conventions are
// inspected: they are claims about the document, not about what it validates.
func decodeSchema(t *testing.T, name string) map[string]any {
	t.Helper()

	var document map[string]any
	if err := json.Unmarshal(readSchema(t, name), &document); err != nil {
		t.Fatalf("decoding %s: %v", name, err)
	}
	return document
}

// definition walks into a schema document and fails the test at the first step
// that is missing, so a rule about a definition can never pass by silently
// finding nothing.
func definition(t *testing.T, document map[string]any, path ...string) map[string]any {
	t.Helper()

	current := document
	for i, step := range path {
		next, ok := current[step].(map[string]any)
		if !ok {
			t.Fatalf("no object at %s", strings.Join(path[:i+1], "/"))
		}
		current = next
	}
	return current
}

// names reads a list of strings out of a schema document, failing the test when
// it is absent or holds anything else — an empty list would make the rules above
// compare nothing against nothing and call it agreement.
func names(t *testing.T, document map[string]any, field string) []string {
	t.Helper()

	values, ok := document[field].([]any)
	if !ok || len(values) == 0 {
		t.Fatalf("%s is missing or empty", field)
	}

	list := make([]string, 0, len(values))
	for _, value := range values {
		name, ok := value.(string)
		if !ok {
			t.Fatalf("%s holds a non-string value: %v", field, value)
		}
		list = append(list, name)
	}
	return list
}

// findKeyword walks a schema document looking for a keyword anywhere in it, and
// reports the JSON-pointer-ish path where it turned up. It descends into objects
// and arrays alike: a keyword nested inside an allOf branch counts exactly as
// much as one at the top.
func findKeyword(node any, keyword, path string) (string, bool) {
	switch value := node.(type) {
	case map[string]any:
		for key, child := range value {
			where := path + "/" + key
			if key == keyword {
				return where, true
			}
			if found, ok := findKeyword(child, keyword, where); ok {
				return found, true
			}
		}
	case []any:
		for i, child := range value {
			if found, ok := findKeyword(child, keyword, fmt.Sprintf("%s/%d", path, i)); ok {
				return found, true
			}
		}
	}
	return "", false
}

// text reads a string out of a schema document, failing the test when it is
// absent or empty rather than letting a rule compare against nothing.
func text(t *testing.T, document map[string]any, field string) string {
	t.Helper()

	value, ok := document[field].(string)
	if !ok || value == "" {
		t.Fatalf("%s is missing or is not a non-empty string", field)
	}
	return value
}

// expectedEnvelope reports which of the three envelopes a schema has to carry.
func expectedEnvelope(schema string) string {
	if ref, ok := envelopeFamilies[schema]; ok {
		return ref
	}
	return envelopeRef
}

// envelopeRefs collects the envelopes a schema composes itself with, so the rule
// above can insist on exactly one rather than on at least one.
func envelopeRefs(document map[string]any) []string {
	known := []string{envelopeRef, documentEnvelopeRef, manifestEnvelopeRef, definitionEnvelopeRef}

	entries, ok := document["allOf"].([]any)
	if !ok {
		return nil
	}

	var carried []string
	for _, entry := range entries {
		subschema, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		ref, ok := subschema["$ref"].(string)
		if ok && slices.Contains(known, ref) {
			carried = append(carried, ref)
		}
	}
	return carried
}

// artifactName turns "task.schema.json" into "task", the directory its fixtures
// live under.
func artifactName(schema string) string {
	return strings.TrimSuffix(schema, ".schema.json")
}

func fixtureDir(schema, kind string) string {
	return filepath.Join("testdata", artifactName(schema), kind)
}

func fixtures(t *testing.T, schema, kind string) []string {
	t.Helper()

	found, err := filepath.Glob(filepath.Join(fixtureDir(schema, kind), "*.json"))
	if err != nil {
		t.Fatalf("listing %s fixtures for %s: %v", kind, schema, err)
	}
	return found
}

func decodeFixture(t *testing.T, path string) any {
	t.Helper()

	// The G304 exemption is narrow: the path comes from a glob over this
	// package's own testdata directory.
	content, err := os.ReadFile(path) //nolint:gosec // fixture path from a glob under testdata
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(content))
	if err != nil {
		t.Fatalf("%s is not valid JSON: %v", path, err)
	}
	return value
}

// locatesProperty reports whether the failure points at a property by name. It
// walks the tree of causes rather than searching the rendered text, because a
// substring matches by accident and the whole point of the rule is that the
// validator knows *where* the artifact is wrong.
//
// A failure inside the envelope drags a cascade behind it: the allOf branch that
// failed loses its annotations, so unevaluatedProperties goes on to reject every
// other envelope field too. Those rejections name fields that are perfectly
// fine, so they only count when they are the whole story — which is exactly what
// an unknown field looks like. Whenever a substantive cause exists, the fixture
// has to be named after one of those.
func locatesProperty(err error, property string) bool {
	var failure *jsonschema.ValidationError
	if !errors.As(err, &failure) {
		return false
	}

	survey := surveyCauses(failure, property)
	if survey.substantive {
		return survey.namedBySubstantive
	}
	return survey.namedByCascade
}

type causeSurvey struct {
	substantive        bool
	namedBySubstantive bool
	namedByCascade     bool
}

func surveyCauses(failure *jsonschema.ValidationError, property string) causeSurvey {
	if len(failure.Causes) > 0 {
		var survey causeSurvey
		for _, cause := range failure.Causes {
			found := surveyCauses(cause, property)
			survey.substantive = survey.substantive || found.substantive
			survey.namedBySubstantive = survey.namedBySubstantive || found.namedBySubstantive
			survey.namedByCascade = survey.namedByCascade || found.namedByCascade
		}
		return survey
	}

	named := namesProperty(failure, property)
	if _, rejected := failure.ErrorKind.(*kind.FalseSchema); rejected {
		return causeSurvey{namedByCascade: named}
	}
	return causeSurvey{substantive: true, namedBySubstantive: named}
}

func namesProperty(failure *jsonschema.ValidationError, property string) bool {
	// Any segment counts, not just the last one: a rule about a map points at the
	// offending entry ('/schemaVersions/task'), and the field the author has to
	// go fix is still the one that named the rule.
	if slices.Contains(failure.InstanceLocation, property) {
		return true
	}

	// Two kinds report against the object rather than against the field, so the
	// name lives in the error kind and not in the location: a missing property,
	// and an unknown one rejected by a nested additionalProperties.
	switch reported := failure.ErrorKind.(type) {
	case *kind.Required:
		return slices.Contains(reported.Missing, property)
	case *kind.AdditionalProperties:
		return slices.Contains(reported.Properties, property)
	default:
		return false
	}
}

func brokenProperty(fixture string) (string, bool) {
	property, _, found := strings.Cut(strings.TrimSuffix(fixture, ".json"), reasonSeparator)
	if !found || property == "" {
		return "", false
	}
	return property, true
}
