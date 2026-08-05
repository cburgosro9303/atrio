package store

import "testing"

func TestCreateAndReadFlowProgress_RoundTrip(t *testing.T) {
	repo := newTestRepository(t)

	id, created, err := repo.CreateFlowProgress(validFlowProgressBusiness())
	if err != nil {
		t.Fatalf("CreateFlowProgress: %v", err)
	}

	read, err := repo.ReadFlowProgress(id)
	if err != nil {
		t.Fatalf("ReadFlowProgress: %v", err)
	}
	if read["flowRef"] != created["flowRef"] {
		t.Fatalf("round trip mismatch: wrote %v, read %v", created, read)
	}
}

func TestUpdateFlowProgress_ClosedStageRequiresOutputRefs(t *testing.T) {
	repo := newTestRepository(t)

	id, _, err := repo.CreateFlowProgress(validFlowProgressBusiness())
	if err != nil {
		t.Fatalf("CreateFlowProgress: %v", err)
	}

	business := validFlowProgressBusiness()
	business["stages"] = []any{
		Document{
			"id":        "explore",
			"state":     "closed",
			"startedAt": "2026-01-01T00:00:00Z",
			"closedAt":  "2026-01-02T00:00:00Z",
		},
	}

	_, err = repo.UpdateFlowProgress(id, business)
	requireRejected(t, err, "stages/0/outputRefs")
}

func TestUpdateFlowProgress_ClosedStageRejectsDanglingOutputRef(t *testing.T) {
	repo := newTestRepository(t)

	id, _, err := repo.CreateFlowProgress(validFlowProgressBusiness())
	if err != nil {
		t.Fatalf("CreateFlowProgress: %v", err)
	}

	business := validFlowProgressBusiness()
	business["stages"] = []any{
		Document{
			"id":        "explore",
			"state":     "closed",
			"startedAt": "2026-01-01T00:00:00Z",
			"closedAt":  "2026-01-02T00:00:00Z",
			"outputRefs": []any{
				Document{"type": "decision", "id": "01J8Z3K2M4N5P6Q7R8S9T0V1W2"},
			},
		},
	}

	_, err = repo.UpdateFlowProgress(id, business)
	requireRejected(t, err, "stages/0/outputRefs/0")
}

func TestUpdateFlowProgress_ClosedStageAcceptsExistingOutputRef(t *testing.T) {
	repo := newTestRepository(t)

	id, _, err := repo.CreateFlowProgress(validFlowProgressBusiness())
	if err != nil {
		t.Fatalf("CreateFlowProgress: %v", err)
	}

	decisionID, _, err := repo.CreateDecision(validDecisionBusiness())
	if err != nil {
		t.Fatalf("CreateDecision: %v", err)
	}

	business := validFlowProgressBusiness()
	business["stages"] = []any{
		Document{
			"id":        "explore",
			"state":     "closed",
			"startedAt": "2026-01-01T00:00:00Z",
			"closedAt":  "2026-01-02T00:00:00Z",
			"outputRefs": []any{
				Document{"type": "decision", "id": decisionID},
			},
		},
	}

	if _, err := repo.UpdateFlowProgress(id, business); err != nil {
		t.Fatalf("UpdateFlowProgress with a valid outputRef: %v", err)
	}
}
