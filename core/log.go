package core

import "time"

// LogEventType is the closed catalog of what a journal entry can record
// (log-entry.schema.json). Widening it is a schema change, not a free edit.
type LogEventType string

const (
	LogEventAgentRunStarted      LogEventType = "agent_run_started"
	LogEventAgentRunFinished     LogEventType = "agent_run_finished"
	LogEventAuthorizationGranted LogEventType = "authorization_granted"
	LogEventAuthorizationDenied  LogEventType = "authorization_denied"
	LogEventMilestone            LogEventType = "milestone"
	LogEventNote                 LogEventType = "note"
)

// LogPayload is the typed detail an event carries. Which fields an event
// actually requires depends on its type; that conditional requirement is
// expressed by the schema (log-entry.schema.json), not by this package.
type LogPayload struct {
	// AgentRef names the agent definition an agent-run or authorization event
	// concerns, as a catalog reference (id@version).
	AgentRef string

	// Capability names which of the permission categories an authorization
	// event was about. The catalog of categories belongs to the permission
	// engine (T-041); this field only carries whatever value it is given.
	Capability string

	// GrantedBy is who decided an authorization. Always a human: an agent
	// cannot authorize itself or another agent.
	GrantedBy Actor

	Command  string
	ExitCode *int
}

// LogEntry is one entry of the project's journal: what happened, who caused
// it, and when.
type LogEntry struct {
	EventType LogEventType
	Summary   string
	Payload   LogPayload
	CreatedBy Actor
	CreatedAt time.Time
}

// Log is an ordered, append-only sequence of journal entries. Enforcing
// append-only against the filesystem — that an entry, once written, is never
// edited — is the store's job (T-020); what belongs here is that the type
// itself offers no operation that could mutate an entry already appended.
// Append and Entries are the only two operations, and both return values
// rather than reaching into the receiver's storage.
type Log struct {
	entries []LogEntry
}

// Append returns a new Log with entry added at the end. The receiver is left
// untouched: there is no way to reach an existing entry and change it.
func (l Log) Append(entry LogEntry) Log {
	next := make([]LogEntry, len(l.entries), len(l.entries)+1)
	copy(next, l.entries)
	next = append(next, entry)
	return Log{entries: next}
}

// Entries returns the log's entries in append order, oldest first. The slice
// is a copy, so mutating it does not reach the log.
func (l Log) Entries() []LogEntry {
	out := make([]LogEntry, len(l.entries))
	copy(out, l.entries)
	return out
}

// Len reports how many entries the log holds.
func (l Log) Len() int {
	return len(l.entries)
}
