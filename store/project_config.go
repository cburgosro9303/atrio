package store

import (
	"fmt"
	"path/filepath"
)

const projectConfigSchema = "project-config.schema.json"

func (r *Repository) projectConfigPath() string {
	return filepath.Join(r.root, atrioDir, "config.json")
}

// ReadProjectConfig loads and validates the project's configuration, stored
// at .atrio/config.json.
func (r *Repository) ReadProjectConfig() (Document, error) {
	return r.readSingleton(r.projectConfigPath(), projectConfigSchema)
}

// WriteProjectConfig creates the project's configuration if none exists yet,
// or replaces it otherwise, preserving id, createdAt and createdBy across an
// update. artifactLanguage is set at creation and rejected if a later write
// tries to change it (schemas/README.md: "artifactLanguage inmutable tras
// la creación | código (T-020)") — checked here by comparing the stored
// version against the incoming one, which is exactly the kind of rule a
// single JSON Schema document cannot see across two of its own versions.
func (r *Repository) WriteProjectConfig(business Document) (Document, error) {
	return r.writeSingleton(r.projectConfigPath(), projectConfigSchema, business, checkArtifactLanguageImmutable)
}

func checkArtifactLanguageImmutable(existing, incoming Document) []FieldError {
	if existing == nil {
		return nil
	}
	was := existing.getString("artifactLanguage")
	got := incoming.getString("artifactLanguage")
	if was == "" || was == got {
		return nil
	}
	return []FieldError{{
		Field:  "artifactLanguage",
		Reason: fmt.Sprintf("cannot change after creation: was %q, got %q", was, got),
	}}
}
