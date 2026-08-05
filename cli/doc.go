// Package cli implements the atrio command line, the primary entry point to the
// platform. It holds no business logic of its own: it parses arguments,
// delegates to the internal API surface, and renders results for a terminal.
//
// Dependency rules (ADR-016): no package of this module may import cli, the one
// exception being cmd/atrio — the main package of the binary, which is itself
// the delivery layer this rule exists to keep downstream.
//
// In the other direction, cli may import api: the portal command starts the
// local HTTP server. That is the only package allowed to do so, and it is what
// makes cmd/atrio reach api transitively; importing api directly from cmd/atrio
// remains a violation. Both exceptions are enforced by internal/archtest.
//
// The full command set (init, sync, task management, agent permissions,
// notifications, token reporting) is built in task T-080.
package cli
