package store

// CreateChangelog validates and writes a new changelog, stored at
// .atrio/management/changelogs/{id}.json.
func (r *Repository) CreateChangelog(business Document) (id string, doc Document, err error) {
	return r.createArtifact(changelogKind, business)
}

// ReadChangelog loads and validates the changelog with the given id.
func (r *Repository) ReadChangelog(id string) (Document, error) {
	return r.readArtifact(changelogKind, id)
}

// ListChangelogIDs lists every changelog id on disk, oldest first.
func (r *Repository) ListChangelogIDs() ([]string, error) {
	return r.listArtifactIDs(changelogKind)
}
