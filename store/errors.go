package store

import (
	"fmt"
	"strings"
)

// FieldError names one field a rejection is about and the reason it was
// rejected. It is the unit the repairable error the platform promises is
// built from (schemas/README.md): a rejection that cannot say which field
// is wrong is not that error.
type FieldError struct {
	// Field is the path to the offending value, JSON-pointer-ish without the
	// leading slash (e.g. "title", "stateHistory/0/to", "agents/1/personalization/displayName").
	// "(root)" names the document itself, for a failure that is not about
	// any one property (e.g. the document as a whole is not an object).
	Field string
	// Reason explains, in one sentence, why Field is wrong.
	Reason string
}

func (f FieldError) String() string {
	return fmt.Sprintf("%s: %s", f.Field, f.Reason)
}

// ArtifactValidationError is returned whenever an artifact is rejected,
// whether the rejection comes from its JSON Schema or from one of the
// code-level rules this package enforces (an immutable field changed, a
// duplicate displayName, a dangling reference, an attempt to overwrite a log
// entry). Every Fields entry names a concrete field and the reason it was
// rejected; nothing here is a dump of the underlying validator.
type ArtifactValidationError struct {
	// Path is the file this artifact was read from or was about to be
	// written to.
	Path   string
	Fields []FieldError
}

func (e *ArtifactValidationError) Error() string {
	parts := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		parts[i] = f.String()
	}
	return fmt.Sprintf("%s: rejected: %s", e.Path, strings.Join(parts, "; "))
}

// NotFoundError is returned when a requested artifact does not exist.
type NotFoundError struct {
	Path string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s: no such artifact", e.Path)
}

// CorruptArtifactError is returned when a file cannot be parsed as JSON at
// all — truncated mid-write, hand-edited into invalid syntax, or otherwise
// not the top-level object every artifact must be. It is kept distinct from
// ArtifactValidationError because there is no field to name: the document
// itself could not be read.
type CorruptArtifactError struct {
	Path string
	Err  error
}

func (e *CorruptArtifactError) Error() string {
	return fmt.Sprintf("%s: not valid JSON: %v", e.Path, e.Err)
}

func (e *CorruptArtifactError) Unwrap() error {
	return e.Err
}
