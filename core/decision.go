package core

import (
	"errors"
	"fmt"
	"reflect"
)

// DecisionStatus is where a decision stands: in force, or replaced by
// another one.
type DecisionStatus string

const (
	DecisionStatusActive     DecisionStatus = "active"
	DecisionStatusSuperseded DecisionStatus = "superseded"
)

// Alternative is an option that was considered for a decision and set aside,
// with the reason it was not chosen.
type Alternative struct {
	Option string
	WhyNot string
}

// Decision is an architecture decision record: the context that prompted it,
// the choice made, and its consequences. It is immutable once created except
// for the single transition to superseded, which must name the decision that
// replaced it (decision.schema.json).
type Decision struct {
	Title                  string
	Context                string
	Choice                 string // The choice made (the schema's "decision" field).
	Consequences           []string
	AlternativesConsidered []Alternative
	Status                 DecisionStatus
	SupersededBy           string // ULID of the replacing decision; set only when Status is superseded.
}

// ValidateDecisionUpdate checks whether replacing previous with next is a
// legal change to a decision. A decision's content is immutable; the only
// legal change is the one-way transition from active to superseded, and that
// transition must name the decision that replaced it. Detecting either rule
// takes comparing two versions of the document, which the schema cannot do
// on its own (schemas/README.md).
func ValidateDecisionUpdate(previous, next Decision) error {
	if previous.Status == DecisionStatusSuperseded {
		// Once superseded, nothing may change at all — not even SupersededBy
		// itself, and not a revival back to active — so this compares every
		// field, unlike the content-only check the other branches use.
		if !reflect.DeepEqual(previous, next) {
			return errors.New("core: a superseded decision cannot be changed further")
		}
		return nil
	}

	if next.Status == previous.Status {
		if !decisionContentEqual(previous, next) {
			return errors.New("core: a decision is immutable except for the transition to superseded")
		}
		return nil
	}

	if previous.Status == DecisionStatusActive && next.Status == DecisionStatusSuperseded {
		if next.SupersededBy == "" {
			return errors.New("core: a superseded decision must name the decision that replaced it")
		}
		if !decisionContentEqual(previous, next) {
			return errors.New("core: superseding a decision may only change its status and supersededBy")
		}
		return nil
	}

	return fmt.Errorf("core: illegal decision status transition from %q to %q", previous.Status, next.Status)
}

// decisionContentEqual reports whether a and b agree on everything except
// Status and SupersededBy, the only two fields superseding is allowed to
// change.
func decisionContentEqual(a, b Decision) bool {
	a.Status, a.SupersededBy = "", ""
	b.Status, b.SupersededBy = "", ""
	return reflect.DeepEqual(a, b)
}
