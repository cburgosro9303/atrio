package store

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"

	"github.com/cburgosro9303/atrio/schemas"
)

// compileSchemas resolves every embedded schema against the others, exactly
// as schemas/schemas_test.go does, so relative $ref between them behaves the
// same in this package as it does in the schemas package's own suite. It
// returns every schema, keyed by file name, not only the ones this package
// manages directly: a schema this package does not compile as an artifact
// (common.schema.json, or the marketplace/definition families that belong to
// a different store) is still needed to register as a resource for the ones
// that reference it.
func compileSchemas() (map[string]*jsonschema.Schema, error) {
	names, err := fs.Glob(schemas.FS, "*.schema.json")
	if err != nil {
		return nil, fmt.Errorf("listing embedded schemas: %w", err)
	}
	if len(names) == 0 {
		return nil, errors.New("no embedded schemas found")
	}

	compiler := jsonschema.NewCompiler()
	// The schemas declare "format": "email" and "format": "date-time" on
	// fields this package itself populates (createdBy, the timestamps) and
	// on fields a hand-edited artifact could corrupt. Format assertions are
	// off by default in this library for drafts 2019-09 and later, which is
	// why schemas/schemas_test.go — a suite about the documents' shape, not
	// about this package's runtime behavior — never turns it on. This is the
	// package that actually gates what gets written and read, so it asserts
	// format for real.
	compiler.AssertFormat()

	for _, name := range names {
		content, err := schemas.FS.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("reading embedded %s: %w", name, err)
		}
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(content))
		if err != nil {
			return nil, fmt.Errorf("%s is not valid JSON: %w", name, err)
		}
		if err := compiler.AddResource(name, document); err != nil {
			return nil, fmt.Errorf("registering %s: %w", name, err)
		}
	}

	compiled := make(map[string]*jsonschema.Schema, len(names))
	for _, name := range names {
		schema, err := compiler.Compile(name)
		if err != nil {
			return nil, fmt.Errorf("compiling %s: %w", name, err)
		}
		compiled[name] = schema
	}
	return compiled, nil
}

// validate checks doc against the named schema and turns a failure into an
// ArtifactValidationError that names every field at fault, or nil on
// success.
func (r *Repository) validate(schemaName, path string, doc Document) error {
	schema, ok := r.schemas[schemaName]
	if !ok {
		return fmt.Errorf("store: no compiled schema for %s", schemaName)
	}

	if err := schema.Validate(plainValue(doc)); err != nil {
		return &ArtifactValidationError{Path: path, Fields: explainValidation(err)}
	}
	return nil
}

// explainValidation turns a jsonschema validation failure into the fields it
// is actually about. A failure inside the envelope drags a cascade behind
// it: the allOf branch that failed loses its annotations under
// unevaluatedProperties, which then rejects every other envelope field too
// (schemas/schemas_test.go documents the same shape of cascade for its own
// test harness, in surveyCauses). Those rejections are kind.FalseSchema and
// name fields that are perfectly fine, so they are kept only as a fallback
// for the case — never observed in practice, but not provably impossible —
// where a failure carries no substantive cause at all.
func explainValidation(err error) []FieldError {
	var failure *jsonschema.ValidationError
	if !errors.As(err, &failure) {
		return []FieldError{{Field: "(document)", Reason: err.Error()}}
	}

	substantive, cascade := splitCauses(failure)
	leaves := substantive
	if len(leaves) == 0 {
		leaves = cascade
	}

	fields := make([]FieldError, 0, len(leaves))
	seen := make(map[string]bool, len(leaves))
	for _, leaf := range leaves {
		for _, fe := range fieldErrorsFor(leaf) {
			key := fe.Field + "\x00" + fe.Reason
			if seen[key] {
				continue
			}
			seen[key] = true
			fields = append(fields, fe)
		}
	}
	if len(fields) == 0 {
		fields = append(fields, FieldError{Field: "(document)", Reason: failure.Error()})
	}
	return fields
}

// splitCauses walks the cause tree down to its leaves (a ValidationError
// with no Causes of its own) and partitions them into substantive failures —
// a real rule that was broken — and the false-schema cascade an envelope
// failure drags behind it under unevaluatedProperties.
func splitCauses(failure *jsonschema.ValidationError) (substantive, cascade []*jsonschema.ValidationError) {
	if len(failure.Causes) > 0 {
		for _, cause := range failure.Causes {
			s, c := splitCauses(cause)
			substantive = append(substantive, s...)
			cascade = append(cascade, c...)
		}
		return substantive, cascade
	}

	if _, isCascade := failure.ErrorKind.(*kind.FalseSchema); isCascade {
		return nil, []*jsonschema.ValidationError{failure}
	}
	return []*jsonschema.ValidationError{failure}, nil
}

// fieldErrorsFor turns one leaf cause into the FieldErrors it names. Two
// kinds report against the containing object rather than against the field
// itself — a missing property and an unknown one rejected by a nested
// additionalProperties — so those two are unpacked into one FieldError per
// name; every other kind already reports at the field's own
// InstanceLocation, and its LocalizedString (surfaced through the leaf's own
// Error method, via the library's default English printer) is reason enough.
func fieldErrorsFor(leaf *jsonschema.ValidationError) []FieldError {
	loc := jsonPointerPath(leaf.InstanceLocation)

	switch k := leaf.ErrorKind.(type) {
	case *kind.Required:
		out := make([]FieldError, 0, len(k.Missing))
		for _, name := range k.Missing {
			out = append(out, FieldError{
				Field:  joinField(loc, name),
				Reason: fmt.Sprintf("required property %q is missing", name),
			})
		}
		return out
	case *kind.AdditionalProperties:
		out := make([]FieldError, 0, len(k.Properties))
		for _, name := range k.Properties {
			out = append(out, FieldError{
				Field:  joinField(loc, name),
				Reason: fmt.Sprintf("unknown property %q is not part of the schema", name),
			})
		}
		return out
	default:
		field := loc
		if field == "" {
			field = "(root)"
		}
		return []FieldError{{Field: field, Reason: leaf.Error()}}
	}
}

func jsonPointerPath(tokens []string) string {
	path := ""
	for i, tok := range tokens {
		if i > 0 {
			path += "/"
		}
		path += tok
	}
	return path
}

func joinField(loc, name string) string {
	if loc == "" {
		return name
	}
	return loc + "/" + name
}
