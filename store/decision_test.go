package store

import "testing"

func TestCreateAndReadDecision_RoundTrip(t *testing.T) {
	repo := newTestRepository(t)

	id, created, err := repo.CreateDecision(validDecisionBusiness())
	if err != nil {
		t.Fatalf("CreateDecision: %v", err)
	}

	read, err := repo.ReadDecision(id)
	if err != nil {
		t.Fatalf("ReadDecision: %v", err)
	}
	if read["title"] != created["title"] || read["id"] != id {
		t.Fatalf("round trip mismatch: wrote %v, read %v", created, read)
	}
}

func TestCreateDecision_RejectsSupersededWithoutSupersededBy(t *testing.T) {
	repo := newTestRepository(t)

	business := validDecisionBusiness()
	business["status"] = "superseded"

	_, _, err := repo.CreateDecision(business)
	requireRejected(t, err, "supersededBy")
}

func TestCreateDecision_RejectsDanglingRef(t *testing.T) {
	repo := newTestRepository(t)

	business := validDecisionBusiness()
	business["refs"] = []any{
		Document{"type": "task", "id": "01J8Z3K2M4N5P6Q7R8S9T0V1W2"},
	}

	_, _, err := repo.CreateDecision(business)
	verr := requireValidationError(t, err, "refs/0")
	if got := verr.Fields[0].Reason; got == "" {
		t.Fatalf("empty reason for a dangling reference")
	}
}

func TestCreateDecision_AcceptsRefToExistingArtifact(t *testing.T) {
	repo := newTestRepository(t)

	taskID, _, err := repo.CreateTask(validTaskBusiness())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	business := validDecisionBusiness()
	business["refs"] = []any{
		Document{"type": "task", "id": taskID},
	}

	if _, _, err := repo.CreateDecision(business); err != nil {
		t.Fatalf("CreateDecision with a valid ref: %v", err)
	}
}

func TestCreateDecision_SkipsDocumentRefExistenceCheck(t *testing.T) {
	repo := newTestRepository(t)

	// "document" refs point into docs/**/*.md, indexed by T-022's SQLite
	// index rather than resolvable by a plain stat; checking those is
	// explicitly out of this package's scope (schemas/README.md).
	business := validDecisionBusiness()
	business["refs"] = []any{
		Document{"type": "document", "id": "01J8Z3K2M4N5P6Q7R8S9T0V1W2"},
	}

	if _, _, err := repo.CreateDecision(business); err != nil {
		t.Fatalf("CreateDecision rejected an unresolvable document ref, which is out of scope: %v", err)
	}
}
