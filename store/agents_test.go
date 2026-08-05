package store

import "testing"

func TestWriteAndReadAgents_RoundTrip(t *testing.T) {
	repo := newTestRepository(t)

	created, err := repo.WriteAgents(validAgentsBusiness())
	if err != nil {
		t.Fatalf("WriteAgents (create): %v", err)
	}

	read, err := repo.ReadAgents()
	if err != nil {
		t.Fatalf("ReadAgents: %v", err)
	}
	if read["id"] != created["id"] {
		t.Fatalf("round trip mismatch: wrote %v, read %v", created, read)
	}
}

func TestWriteAgents_RejectsDuplicateDisplayName(t *testing.T) {
	repo := newTestRepository(t)

	business := Document{
		"agents": []any{
			Document{"ref": "architect@1.0.0", "personalization": Document{"displayName": "Andres"}},
			Document{"ref": "product-owner@1.0.0", "personalization": Document{"displayName": "Andres"}},
		},
	}

	_, err := repo.WriteAgents(business)
	verr := requireValidationError(t, err, "agents/1/personalization/displayName")
	if got := verr.Fields[0].Reason; got == "" {
		t.Fatalf("empty reason for a duplicate displayName")
	}
}

func TestWriteAgents_DistinctDisplayNamesAccepted(t *testing.T) {
	repo := newTestRepository(t)

	business := Document{
		"agents": []any{
			Document{"ref": "architect@1.0.0", "personalization": Document{"displayName": "Andres"}},
			Document{"ref": "product-owner@1.0.0", "personalization": Document{"displayName": "Marta"}},
		},
	}

	if _, err := repo.WriteAgents(business); err != nil {
		t.Fatalf("WriteAgents with distinct displayNames: %v", err)
	}
}
