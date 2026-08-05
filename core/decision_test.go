package core_test

import (
	"testing"

	"github.com/cburgosro9303/atrio/core"
)

func baseDecision() core.Decision {
	return core.Decision{
		Title:        "Use ULIDs for artifact identifiers",
		Context:      "Artifacts need an identifier that sorts chronologically.",
		Choice:       "Use ULID for every artifact.",
		Consequences: []string{"IDs are sortable by creation time."},
		Status:       core.DecisionStatusActive,
	}
}

func TestValidateDecisionUpdate(t *testing.T) {
	t.Parallel()

	t.Run("identical copy is legal", func(t *testing.T) {
		t.Parallel()

		d := baseDecision()
		if err := core.ValidateDecisionUpdate(d, d); err != nil {
			t.Errorf("ValidateDecisionUpdate() error = %v, want nil", err)
		}
	})

	t.Run("content change while active is rejected", func(t *testing.T) {
		t.Parallel()

		before := baseDecision()
		after := before
		after.Consequences = []string{"changed my mind"}
		if err := core.ValidateDecisionUpdate(before, after); err == nil {
			t.Error("ValidateDecisionUpdate() should reject a content change to an active decision")
		}
	})

	t.Run("superseding without naming the replacement is rejected", func(t *testing.T) {
		t.Parallel()

		before := baseDecision()
		after := before
		after.Status = core.DecisionStatusSuperseded
		// after.SupersededBy is deliberately left empty.
		if err := core.ValidateDecisionUpdate(before, after); err == nil {
			t.Error("ValidateDecisionUpdate() should reject superseding without naming the replacement")
		}
	})

	t.Run("superseding while naming the replacement is legal", func(t *testing.T) {
		t.Parallel()

		before := baseDecision()
		after := before
		after.Status = core.DecisionStatusSuperseded
		after.SupersededBy = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
		if err := core.ValidateDecisionUpdate(before, after); err != nil {
			t.Errorf("ValidateDecisionUpdate() error = %v, want nil", err)
		}
	})

	t.Run("superseding may not also change content", func(t *testing.T) {
		t.Parallel()

		before := baseDecision()
		after := before
		after.Status = core.DecisionStatusSuperseded
		after.SupersededBy = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
		after.Title = "A different title"
		if err := core.ValidateDecisionUpdate(before, after); err == nil {
			t.Error("ValidateDecisionUpdate() should reject a content change bundled with superseding")
		}
	})

	t.Run("an identical copy of a superseded decision is legal", func(t *testing.T) {
		t.Parallel()

		superseded := baseDecision()
		superseded.Status = core.DecisionStatusSuperseded
		superseded.SupersededBy = "01ARZ3NDEKTSV4RRFFQ69G5FAV"

		copyOfSuperseded := superseded
		if err := core.ValidateDecisionUpdate(superseded, copyOfSuperseded); err != nil {
			t.Errorf("ValidateDecisionUpdate() error = %v, want nil for an unchanged superseded decision", err)
		}
	})

	t.Run("a superseded decision cannot change further", func(t *testing.T) {
		t.Parallel()

		superseded := baseDecision()
		superseded.Status = core.DecisionStatusSuperseded
		superseded.SupersededBy = "01ARZ3NDEKTSV4RRFFQ69G5FAV"

		after := superseded
		after.SupersededBy = "01BX5ZZKBKACTAV9WEVGEMMVRZ"
		if err := core.ValidateDecisionUpdate(superseded, after); err == nil {
			t.Error("ValidateDecisionUpdate() should reject any change to an already-superseded decision")
		}
	})

	t.Run("an unrecognized status transition is rejected", func(t *testing.T) {
		t.Parallel()

		// DecisionStatus is a named string, not a closed Go enum, so a value
		// outside {active, superseded} can still reach this function (e.g.
		// forwarded from an artifact whose schema validation was bypassed).
		// The fallback must still reject it rather than silently accept it.
		before := baseDecision()
		before.Status = "unknown"
		after := before
		after.Status = core.DecisionStatusActive
		if err := core.ValidateDecisionUpdate(before, after); err == nil {
			t.Error("ValidateDecisionUpdate() should reject a transition out of an unrecognized status")
		}
	})

	t.Run("reviving a superseded decision is rejected", func(t *testing.T) {
		t.Parallel()

		superseded := baseDecision()
		superseded.Status = core.DecisionStatusSuperseded
		superseded.SupersededBy = "01ARZ3NDEKTSV4RRFFQ69G5FAV"

		revived := superseded
		revived.Status = core.DecisionStatusActive
		revived.SupersededBy = ""
		if err := core.ValidateDecisionUpdate(superseded, revived); err == nil {
			t.Error("ValidateDecisionUpdate() should reject reviving a superseded decision")
		}
	})
}
