package store

// CreateTask validates and writes a new task or bug, stored at
// .atrio/management/tasks/{id}.json. business holds the task's own fields
// (type, title, state, stateHistory, ...); the envelope is assigned here.
func (r *Repository) CreateTask(business Document) (id string, doc Document, err error) {
	return r.createArtifact(taskKind, business)
}

// ReadTask loads and validates the task with the given id.
func (r *Repository) ReadTask(id string) (Document, error) {
	return r.readArtifact(taskKind, id)
}

// UpdateTask replaces a task's business fields, preserving its id, createdAt
// and createdBy.
func (r *Repository) UpdateTask(id string, business Document) (Document, error) {
	return r.updateArtifact(taskKind, id, business)
}

// ListTaskIDs lists every task and bug id on disk, oldest first.
func (r *Repository) ListTaskIDs() ([]string, error) {
	return r.listArtifactIDs(taskKind)
}
