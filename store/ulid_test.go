package store

import (
	"regexp"
	"testing"
)

// canonicalULID mirrors common.schema.json#/$defs/ulid exactly, independent
// of the pattern this package itself uses to validate ids, so a bug shared
// between ulidPattern and its test would not go unnoticed.
var canonicalULID = regexp.MustCompile(`^[0-7][0-9A-HJKMNP-TV-Z]{25}$`)

func TestIDGenerator_ProducesWellFormedULIDs(t *testing.T) {
	gen := newIDGenerator()

	for i := 0; i < 20; i++ {
		id, err := gen.next()
		if err != nil {
			t.Fatalf("next() #%d: %v", i, err)
		}
		if len(id) != 26 {
			t.Fatalf("id %q is %d characters, want 26", id, len(id))
		}
		if !canonicalULID.MatchString(id) {
			t.Fatalf("id %q does not match the canonical ULID form", id)
		}
	}
}

func TestIDGenerator_MonotonicWithinSameMillisecond(t *testing.T) {
	gen := newIDGenerator()

	const n = 200
	ids := make([]string, n)
	for i := range ids {
		id, err := gen.next()
		if err != nil {
			t.Fatalf("next() #%d: %v", i, err)
		}
		ids[i] = id
	}

	// Generating 200 ids back to back all but guarantees several of them
	// share a millisecond; every one still has to sort strictly after the
	// one before it, since directory listing order (a plain lexicographic
	// sort of file names) is this package's promise of chronological order.
	for i := 1; i < len(ids); i++ {
		if ids[i-1] >= ids[i] {
			t.Fatalf("ids[%d] = %s is not strictly less than ids[%d] = %s", i-1, ids[i-1], i, ids[i])
		}
	}
}

func TestIsWellFormedULID(t *testing.T) {
	cases := []struct {
		id    string
		valid bool
	}{
		{"01J8Z3K2M4N5P6Q7R8S9T0V1W2", true},
		{"", false},
		{"not-a-ulid", false},
		{"01J8Z3K2M4N5P6Q7R8S9T0V1W", false},   // 25 chars
		{"01J8Z3K2M4N5P6Q7R8S9T0V1W22", false}, // 27 chars
		{"01j8z3k2m4n5p6q7r8s9t0v1w2", false},  // lowercase
		{"01J8Z3K2M4N5P6Q7R8S9T0V1WI", false},  // excluded letter I
		{"../../etc/passwd", false},
	}
	for _, c := range cases {
		if got := isWellFormedULID(c.id); got != c.valid {
			t.Errorf("isWellFormedULID(%q) = %v, want %v", c.id, got, c.valid)
		}
	}
}

func TestReadTask_RejectsMalformedID(t *testing.T) {
	repo := newTestRepository(t)

	if _, err := repo.ReadTask("../../etc/passwd"); err == nil {
		t.Fatal("ReadTask accepted a malformed id that could escape the management folder")
	}
}
