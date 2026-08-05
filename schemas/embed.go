// Package schemas holds the JSON Schemas that define Atrio's artifacts.
//
// The schemas are the published contract (ADR-005), so they live at the root of
// the module where anyone can find them, and they are embedded rather than read
// from disk: a binary that validates against schemas it might not find is a
// binary that can silently stop validating.
//
// The package is a leaf. It imports nothing from the module, and the code that
// compiles and applies these schemas lives in store, which owns validation.
package schemas

import "embed"

// FS holds every schema file, keyed by its name at the root of this package
// (for instance "task.schema.json"). The names double as the schemas' relative
// $id, so a $ref between two of them resolves without a base URI — which is
// deliberate: Atrio has no domain of its own yet, and a $id pointing at a URL
// nobody controls would be a promise the project cannot keep.
//
//go:embed *.schema.json
var FS embed.FS
