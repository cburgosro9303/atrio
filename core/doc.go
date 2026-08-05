// Package core holds the pure domain of Atrio: entities, state machines and
// business rules for tasks, decisions, journal entries, changelogs and the
// agent permission model.
//
// Dependency rule: core imports no other package of this module. It performs no
// I/O whatsoever — no filesystem, no network, no process execution, no clock or
// randomness sourced from the outside. Everything it needs is passed in by the
// caller, which is what makes its logic exhaustively testable.
//
// The rule is enforced by the architecture test in internal/archtest, for
// production and test code alike.
package core
