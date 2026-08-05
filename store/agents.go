package store

import (
	"fmt"
	"path/filepath"
)

const agentsSchema = "agents.schema.json"

func (r *Repository) agentsPath() string {
	return filepath.Join(r.root, atrioDir, "agents.json")
}

// ReadAgents loads and validates the project's agent roster, stored at
// .atrio/agents.json.
func (r *Repository) ReadAgents() (Document, error) {
	return r.readSingleton(r.agentsPath(), agentsSchema)
}

// WriteAgents creates the project's agent roster if none exists yet, or
// replaces it otherwise, preserving id, createdAt and createdBy across an
// update. displayName must be unique across every agent declared in the
// document (agents.schema.json's own description: "enforced by the core,
// not by this schema — it is a rule between siblings inside one document,
// which a JSON Schema document cannot check against itself"); that rule is
// the second one schemas/README.md assigns to this package by name
// ("Unicidad de displayName entre agentes | código (T-020/T-041)").
func (r *Repository) WriteAgents(business Document) (Document, error) {
	return r.writeSingleton(r.agentsPath(), agentsSchema, business, checkUniqueDisplayNames)
}

func checkUniqueDisplayNames(_, incoming Document) []FieldError {
	agents, ok := incoming["agents"].([]any)
	if !ok {
		return nil
	}

	var fields []FieldError
	seen := make(map[string]int, len(agents))
	for i, entry := range agents {
		agent, ok := asMap(entry)
		if !ok {
			continue
		}
		personalization, ok := asMap(agent["personalization"])
		if !ok {
			continue
		}
		name, ok := personalization["displayName"].(string)
		if !ok || name == "" {
			continue
		}

		if first, duplicate := seen[name]; duplicate {
			fields = append(fields, FieldError{
				Field:  fmt.Sprintf("agents/%d/personalization/displayName", i),
				Reason: fmt.Sprintf("duplicate displayName %q; already used by agents/%d", name, first),
			})
			continue
		}
		seen[name] = i
	}
	return fields
}
