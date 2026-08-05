package store

import "testing"

func TestWriteAndReadProjectConfig_RoundTrip(t *testing.T) {
	repo := newTestRepository(t)

	created, err := repo.WriteProjectConfig(validProjectConfigBusiness())
	if err != nil {
		t.Fatalf("WriteProjectConfig (create): %v", err)
	}
	if id, ok := created["id"].(string); !ok || !isWellFormedULID(id) {
		t.Fatalf("WriteProjectConfig did not assign a well-formed id: %v", created["id"])
	}

	read, err := repo.ReadProjectConfig()
	if err != nil {
		t.Fatalf("ReadProjectConfig: %v", err)
	}
	if read["name"] != created["name"] || read["id"] != created["id"] {
		t.Fatalf("round trip mismatch: wrote %v, read %v", created, read)
	}
}

func TestWriteProjectConfig_SecondWriteIsAnUpdate(t *testing.T) {
	repo := newTestRepository(t)

	created, err := repo.WriteProjectConfig(validProjectConfigBusiness())
	if err != nil {
		t.Fatalf("WriteProjectConfig (create): %v", err)
	}

	business := validProjectConfigBusiness()
	business["description"] = "An updated description."

	updated, err := repo.WriteProjectConfig(business)
	if err != nil {
		t.Fatalf("WriteProjectConfig (update): %v", err)
	}

	if updated["id"] != created["id"] {
		t.Errorf("id changed across update: %v -> %v", created["id"], updated["id"])
	}
	if updated["createdAt"] != created["createdAt"] {
		t.Errorf("createdAt changed across update: %v -> %v", created["createdAt"], updated["createdAt"])
	}
	if updated["description"] != "An updated description." {
		t.Errorf("business field did not take: description = %v", updated["description"])
	}
}

func TestWriteProjectConfig_RejectsArtifactLanguageChange(t *testing.T) {
	repo := newTestRepository(t)

	if _, err := repo.WriteProjectConfig(validProjectConfigBusiness()); err != nil {
		t.Fatalf("WriteProjectConfig (create): %v", err)
	}

	business := validProjectConfigBusiness()
	business["artifactLanguage"] = "en"

	_, err := repo.WriteProjectConfig(business)
	requireRejected(t, err, "artifactLanguage")
}

func TestWriteProjectConfig_SameLanguageIsNotAChange(t *testing.T) {
	repo := newTestRepository(t)

	if _, err := repo.WriteProjectConfig(validProjectConfigBusiness()); err != nil {
		t.Fatalf("WriteProjectConfig (create): %v", err)
	}

	if _, err := repo.WriteProjectConfig(validProjectConfigBusiness()); err != nil {
		t.Fatalf("WriteProjectConfig (rewriting the same artifactLanguage): %v", err)
	}
}
