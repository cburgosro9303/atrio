// Package flows implements the generic, deterministic engine that executes
// declarative flow definitions. Flows are data distributed through the
// marketplace, not code: adding a flow must never require changing this engine.
//
// The engine loads a definition, runs the per-stage state machine
// (pending -> in progress -> pending closure -> closed | skipped), persists
// versioned progress so a teammate can resume an interrupted run, computes the
// blocking-data checklist and asks only about the gaps, and validates the
// structured output an agent produces at stage closure against its schema.
//
// Agents converse and produce; they never decide the course. The moderator is
// programmatic.
package flows
