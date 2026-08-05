package store

import "fmt"

// Identity supplies the git identity a repository attributes its writes to.
// The common envelope's createdBy (common.schema.json#/$defs/actor) requires
// both halves of a human's identity — user.name and user.email — because the
// platform blocks until git identity is complete, so an attributed change
// always carries both.
//
// store does not read git itself: detecting the git binary, parsing
// `git config`, and prompting for a missing identity is gitops's job (T-030),
// developed in parallel with this package. This interface is the seam
// between the two: whoever wires a Repository together supplies an
// implementation, typically one backed by gitops.
type Identity interface {
	// Name returns the configured git user.name.
	Name() (string, error)
	// Email returns the configured git user.email.
	Email() (string, error)
}

// actor builds the createdBy value the repository attributes new and
// updated artifacts to: a human, identified by the injected git identity.
// store never attributes a write to an agent — an agent-authored artifact
// still passes through a human's local git identity, because that identity
// is what commits the write to the repository.
func (r *Repository) actor() (Document, error) {
	name, err := r.identity.Name()
	if err != nil {
		return nil, fmt.Errorf("resolving git user.name: %w", err)
	}
	email, err := r.identity.Email()
	if err != nil {
		return nil, fmt.Errorf("resolving git user.email: %w", err)
	}
	return Document{"kind": "human", "name": name, "email": email}, nil
}
