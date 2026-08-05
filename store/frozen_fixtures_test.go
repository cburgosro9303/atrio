package store

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// frozenFixtureDirs maps each schema this package manages to the testdata
// folder schemas/schemas_test.go exercises it against (T-010/T-011/T-012).
var frozenFixtureDirs = map[string]string{
	"task.schema.json":           "task",
	"decision.schema.json":       "decision",
	"log-entry.schema.json":      "log-entry",
	"changelog.schema.json":      "changelog",
	"flow-progress.schema.json":  "flow-progress",
	"project-config.schema.json": "project-config",
	"agents.schema.json":         "agents",
}

// TestCompiledSchemas_AcceptEveryFrozenValidFixture guards a real risk in
// validate.go's decision to call compiler.AssertFormat(): schemas'
// own suite (schemas/schemas_test.go) compiles with format assertions off,
// which is the library's default for draft 2019-09 and later, so no
// T-010/T-011/T-012 fixture has ever been validated with them on. Turning
// them on here, in the package that is the actual production gate, must not
// narrow the contract those tasks already froze as public — if it did, that
// would be a contradiction to report, not to silently paper over by editing
// a frozen fixture. This test is the empirical check: every "valid" fixture
// for a kind this package manages has to keep validating once format
// assertions are on.
func TestCompiledSchemas_AcceptEveryFrozenValidFixture(t *testing.T) {
	compiled, err := compileSchemas()
	if err != nil {
		t.Fatalf("compileSchemas: %v", err)
	}

	for schemaName, dir := range frozenFixtureDirs {
		schema, ok := compiled[schemaName]
		if !ok {
			t.Fatalf("no compiled schema for %s", schemaName)
		}

		pattern := filepath.Join("..", "schemas", "testdata", dir, "valid", "*.json")
		fixtures, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("listing fixtures at %s: %v", pattern, err)
		}
		if len(fixtures) == 0 {
			t.Fatalf("no valid fixtures found at %s; the frozenFixtureDirs mapping or the schemas "+
				"package's testdata layout has drifted", pattern)
		}

		for _, fixture := range fixtures {
			t.Run(schemaName+"/"+filepath.Base(fixture), func(t *testing.T) {
				content, err := os.ReadFile(fixture) //nolint:gosec // fixture path from a glob under this module's own schemas/testdata
				if err != nil {
					t.Fatalf("reading %s: %v", fixture, err)
				}
				value, err := jsonschema.UnmarshalJSON(bytes.NewReader(content))
				if err != nil {
					t.Fatalf("%s is not valid JSON: %v", fixture, err)
				}
				if err := schema.Validate(value); err != nil {
					t.Errorf("AssertFormat narrowed the frozen contract: %s was accepted by "+
						"schemas' own suite but this package's stricter compiler rejects it:\n%v", fixture, err)
				}
			})
		}
	}
}
