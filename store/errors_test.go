package store

import (
	"errors"
	"testing"
)

func TestReadTask_MissingFile(t *testing.T) {
	repo := newTestRepository(t)

	_, err := repo.ReadTask("01J8Z3K2M4N5P6Q7R8S9T0V1W2")

	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("got error %T (%v), want *NotFoundError", err, err)
	}
}

func TestReadTask_CorruptJSON(t *testing.T) {
	repo := newTestRepository(t)

	id := "01J8Z3K2M4N5P6Q7R8S9T0V1W2"
	writeRawFile(t, repo.artifactPath(taskKind, id), []byte("this is not json at all"))

	_, err := repo.ReadTask(id)

	var corrupt *CorruptArtifactError
	if !errors.As(err, &corrupt) {
		t.Fatalf("got error %T (%v), want *CorruptArtifactError", err, err)
	}
}

func TestReadTask_TruncatedJSON(t *testing.T) {
	repo := newTestRepository(t)

	id := "01J8Z3K2M4N5P6Q7R8S9T0V1W2"
	// A file cut off mid-write: valid JSON up to the point the process died,
	// then nothing. This is exactly the shape a crash during a naive,
	// non-atomic write would leave behind.
	writeRawFile(t, repo.artifactPath(taskKind, id), []byte(`{"schemaVersion": 1, "id": "01J8Z3K2M4N5P6Q7R8S9T0V1W2", "type": "task", "tit`))

	_, err := repo.ReadTask(id)

	var corrupt *CorruptArtifactError
	if !errors.As(err, &corrupt) {
		t.Fatalf("got error %T (%v), want *CorruptArtifactError", err, err)
	}
}

func TestReadTask_TopLevelArrayIsCorrupt(t *testing.T) {
	repo := newTestRepository(t)

	id := "01J8Z3K2M4N5P6Q7R8S9T0V1W2"
	writeRawFile(t, repo.artifactPath(taskKind, id), []byte(`["not", "an", "object"]`))

	_, err := repo.ReadTask(id)

	var corrupt *CorruptArtifactError
	if !errors.As(err, &corrupt) {
		t.Fatalf("got error %T (%v), want *CorruptArtifactError", err, err)
	}
}

func TestOpen_RejectsNilIdentity(t *testing.T) {
	if _, err := Open(t.TempDir(), nil); err == nil {
		t.Fatal("Open accepted a nil identity")
	}
}
