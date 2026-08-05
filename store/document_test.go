package store

import (
	"strings"
	"testing"
)

func TestPlainValue_NormalizesNestedDocumentsAndArrays(t *testing.T) {
	nested := Document{
		"a": Document{"b": "c"},
		"d": []any{Document{"e": "f"}, "g"},
	}

	got, ok := plainValue(nested).(map[string]any)
	if !ok {
		t.Fatalf("top level is %T, want map[string]any", plainValue(nested))
	}

	a, ok := got["a"].(map[string]any)
	if !ok {
		t.Fatalf("nested Document under \"a\" is %T, not normalized to map[string]any", got["a"])
	}
	if a["b"] != "c" {
		t.Fatalf("value lost while normalizing: %v", a)
	}

	d, ok := got["d"].([]any)
	if !ok {
		t.Fatalf("\"d\" is %T, want []any", got["d"])
	}
	if _, ok := d[0].(map[string]any); !ok {
		t.Fatalf("Document nested inside an array was not normalized: %#v", d[0])
	}
}

// TestUnrecognizedGoValue_FailsValidationLoudly pins down a property this
// package depends on but does not implement itself: a Go value plainValue
// does not know how to normalize — its default case returns it unchanged —
// has to make the underlying jsonschema validator fail loudly with "invalid
// jsonType" rather than being silently treated as some valid JSON type.
// This is exactly the failure mode that flagged store.Document itself
// during this package's development (a nested Document value reported
// "invalid jsonType store.Document" before plainValue existed to normalize
// it): the fix depends on the validator continuing to fail closed on a Go
// type it does not recognize, and that is third-party behavior, not
// something this package controls. If a future version of the library
// relaxed it — treating an unknown type as, say, an empty object — an
// artifact holding an unrepresentable value could validate and be written
// to disk silently. This test exists so that regression shows up as a red
// test here, not as a corrupt artifact discovered later.
func TestUnrecognizedGoValue_FailsValidationLoudly(t *testing.T) {
	repo := newTestRepository(t)

	business := validTaskBusiness()
	business["title"] = complex128(1 + 2i) // no JSON representation; plainValue's default case passes it through unchanged

	_, _, err := repo.CreateTask(business)
	verr := requireValidationError(t, err, "title")

	field, _ := findField(verr.Fields, "title")
	if !strings.Contains(field.Reason, "invalid jsonType") {
		t.Fatalf("want the rejection reason to mention \"invalid jsonType\" (kind.InvalidJsonValue), got: %q", field.Reason)
	}
}
