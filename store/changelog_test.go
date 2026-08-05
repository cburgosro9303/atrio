package store

import "testing"

func TestCreateAndReadChangelog_RoundTrip(t *testing.T) {
	repo := newTestRepository(t)

	taskID, _, err := repo.CreateTask(validTaskBusiness())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	id, created, err := repo.CreateChangelog(validChangelogBusiness(taskID))
	if err != nil {
		t.Fatalf("CreateChangelog: %v", err)
	}

	read, err := repo.ReadChangelog(id)
	if err != nil {
		t.Fatalf("ReadChangelog: %v", err)
	}
	if read["summary"] != created["summary"] {
		t.Fatalf("round trip mismatch: wrote %v, read %v", created, read)
	}
}

func TestCreateChangelog_RejectsRenameWithoutPreviousPath(t *testing.T) {
	repo := newTestRepository(t)

	business := validChangelogBusiness("01J8Z3K2M4N5P6Q7R8S9T0V1W2")
	business["changes"] = []any{
		Document{"path": "b.go", "kind": "renamed", "description": "moved"},
	}

	_, _, err := repo.CreateChangelog(business)
	requireRejected(t, err, "changes/0/previousPath")
}

func TestCreateChangelog_RejectsDanglingImpact(t *testing.T) {
	repo := newTestRepository(t)

	business := validChangelogBusiness("01J8Z3K2M4N5P6Q7R8S9T0V1W2")
	business["impacts"] = []any{
		Document{"type": "decision", "id": "01J8Z3K2M4N5P6Q7R8S9T0V1W2"},
	}

	_, _, err := repo.CreateChangelog(business)
	requireRejected(t, err, "impacts/0")
}
