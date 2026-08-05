package store

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"time"

	"github.com/oklog/ulid/v2"
)

// ulidPattern mirrors common.schema.json#/$defs/ulid: 26 characters of
// Crockford base32, uppercase, the first bounded by the largest
// representable timestamp. It guards every path built from a caller-supplied
// id before that id ever reaches the filesystem, so a malformed id is
// rejected with a clear reason instead of being handed to os.Open as a path
// fragment that could contain ".." or a separator.
var ulidPattern = regexp.MustCompile(`^[0-7][0-9A-HJKMNP-TV-Z]{25}$`)

func isWellFormedULID(id string) bool {
	return ulidPattern.MatchString(id)
}

// idGenerator produces sortable, monotonically increasing ULIDs: two
// artifacts created within the same millisecond still sort in creation
// order, because ulid.Monotonic increments the entropy bytes instead of
// drawing them fresh. That ordering is a real property this package
// promises — the log's directory order is its chronological order
// (log-entry.schema.json) — not merely cosmetic.
//
// LockedMonotonicReader wraps the monotonic source with a mutex, which is
// what makes a single idGenerator safe to share across concurrent Create
// calls; ulid.MonotonicEntropy itself is documented as unsafe for that.
type idGenerator struct {
	entropy *ulid.LockedMonotonicReader
}

func newIDGenerator() *idGenerator {
	return &idGenerator{
		entropy: &ulid.LockedMonotonicReader{
			MonotonicReader: ulid.Monotonic(rand.Reader, 0),
		},
	}
}

// next returns a new ULID as its canonical 26-character string form.
func (g *idGenerator) next() (string, error) {
	id, err := ulid.New(ulid.Timestamp(time.Now()), g.entropy)
	if err != nil {
		return "", fmt.Errorf("generating ulid: %w", err)
	}
	return id.String(), nil
}
