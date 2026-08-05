package store

// CreateFlowProgress validates and writes new flow execution progress,
// stored at .atrio/management/flows/{id}.json.
func (r *Repository) CreateFlowProgress(business Document) (id string, doc Document, err error) {
	return r.createArtifact(flowProgressKind, business)
}

// ReadFlowProgress loads and validates the flow progress with the given id.
func (r *Repository) ReadFlowProgress(id string) (Document, error) {
	return r.readArtifact(flowProgressKind, id)
}

// UpdateFlowProgress replaces a flow progress document's business fields —
// its stages — preserving its id, createdAt and createdBy. The legal order
// of stage-state transitions is enforced by the flow engine (T-070), not
// here.
func (r *Repository) UpdateFlowProgress(id string, business Document) (Document, error) {
	return r.updateArtifact(flowProgressKind, id, business)
}

// ListFlowProgressIDs lists every flow progress id on disk, oldest first.
func (r *Repository) ListFlowProgressIDs() ([]string, error) {
	return r.listArtifactIDs(flowProgressKind)
}
