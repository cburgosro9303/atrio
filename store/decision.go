package store

// CreateDecision validates and writes a new decision, stored at
// .atrio/management/decisions/{id}.json.
func (r *Repository) CreateDecision(business Document) (id string, doc Document, err error) {
	return r.createArtifact(decisionKind, business)
}

// ReadDecision loads and validates the decision with the given id.
func (r *Repository) ReadDecision(id string) (Document, error) {
	return r.readArtifact(decisionKind, id)
}

// UpdateDecision replaces a decision's business fields, preserving its id,
// createdAt and createdBy. Whether the requested change is legal — a
// decision is immutable except for the transition to superseded — is
// enforced by the core (T-040), which compares the previous version with
// the one it is about to write before calling this.
func (r *Repository) UpdateDecision(id string, business Document) (Document, error) {
	return r.updateArtifact(decisionKind, id, business)
}

// ListDecisionIDs lists every decision id on disk, oldest first.
func (r *Repository) ListDecisionIDs() ([]string, error) {
	return r.listArtifactIDs(decisionKind)
}
