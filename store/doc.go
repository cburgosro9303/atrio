// Package store implements Atrio's persistence layer over the two writable
// stores: versioned JSON artifacts living in the repository's management folder
// (the shared source of truth) and the local SQLite database, which holds only
// derivable or ephemeral state and is never committed.
//
// Every JSON artifact is validated against its JSON Schema on read and on
// write, and carries a schemaVersion. SQLite is accessed through the pure-Go
// driver modernc.org/sqlite: CGo is forbidden because it breaks cross-compilation.
//
// The invariant this package must uphold: deleting the database, cloning the
// repository and running sync reconstructs 100% of the local state.
package store
