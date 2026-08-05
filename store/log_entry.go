package store

// CreateLogEntry validates and appends a new entry to the project log,
// stored at .atrio/management/log/{id}.json. There is deliberately no
// UpdateLogEntry: the log is append-only (log-entry.schema.json), and the
// underlying write refuses outright if a file already exists at the target
// id (writeNew, in fsio.go) rather than relying on this package never
// calling anything that could overwrite one.
func (r *Repository) CreateLogEntry(business Document) (id string, doc Document, err error) {
	return r.createArtifact(logEntryKind, business)
}

// ReadLogEntry loads and validates the log entry with the given id.
func (r *Repository) ReadLogEntry(id string) (Document, error) {
	return r.readArtifact(logEntryKind, id)
}

// ListLogEntryIDs lists every log entry id on disk. Because entries are
// named by their own sortable ULID, this order is also the order the events
// happened in.
func (r *Repository) ListLogEntryIDs() ([]string, error) {
	return r.listArtifactIDs(logEntryKind)
}
