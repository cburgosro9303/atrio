package core_test

import (
	"testing"

	"github.com/cburgosro9303/atrio/core"
)

func TestLog_AppendOnly(t *testing.T) {
	t.Parallel()

	by := humanActor("cesar")
	first := core.LogEntry{EventType: core.LogEventMilestone, Summary: "flow started", CreatedBy: by, CreatedAt: fixedTime}
	second := core.LogEntry{EventType: core.LogEventNote, Summary: "checkpoint", CreatedBy: by, CreatedAt: fixedTime}

	var log core.Log
	if log.Len() != 0 {
		t.Fatalf("a zero-value Log should be empty, got Len() = %d", log.Len())
	}

	afterFirst := log.Append(first)
	if afterFirst.Len() != 1 {
		t.Fatalf("Len() after one append = %d, want 1", afterFirst.Len())
	}
	// Append returns a new value; the receiver is untouched.
	if log.Len() != 0 {
		t.Errorf("the original log was mutated by Append(), Len() = %d, want 0", log.Len())
	}

	afterSecond := afterFirst.Append(second)
	if afterSecond.Len() != 2 {
		t.Fatalf("Len() after two appends = %d, want 2", afterSecond.Len())
	}
	if afterFirst.Len() != 1 {
		t.Errorf("appending to afterFirst mutated it, Len() = %d, want 1", afterFirst.Len())
	}

	entries := afterSecond.Entries()
	if len(entries) != 2 || entries[0].Summary != first.Summary || entries[1].Summary != second.Summary {
		t.Fatalf("Entries() = %+v, want [%+v, %+v] in append order", entries, first, second)
	}

	// Entries() hands back a copy: mutating it must not reach the log.
	entries[0].Summary = "tampered"
	if got := afterSecond.Entries()[0].Summary; got != first.Summary {
		t.Errorf("mutating the slice from Entries() leaked into the log, Summary = %q", got)
	}
}
