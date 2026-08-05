package gitops

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Minimum{Major,Minor,Patch} declare the lowest git version this package
// accepts, as package constants. ADR-004 declares the system git binary a
// prerequisite of the platform but leaves the exact minimum version number to
// the implementation.
//
// 2.30.0 (December 2020) is chosen as a floor that is old enough to be
// available on every platform Atrio targets — including default package
// repositories of long-term-support Linux distributions still in use — while
// being new enough that `git status --porcelain=v1 -z` and `git config --get`
// behave exactly as documented; both have been stable for far longer than
// this floor. It is a single lower bound, not a range: unlike a provider CLI
// (section 7 of the architecture doc), git is a system prerequisite the
// platform does not version-pin against a moving target.
const (
	MinimumVersionMajor = 2
	MinimumVersionMinor = 30
	MinimumVersionPatch = 0
)

// MinimumVersion is MinimumVersionMajor.MinimumVersionMinor.MinimumVersionPatch
// as a Version, for direct comparison against a detected Binary.Version.
var MinimumVersion = Version{
	Major: MinimumVersionMajor,
	Minor: MinimumVersionMinor,
	Patch: MinimumVersionPatch,
}

// Version is a parsed git release number.
type Version struct {
	Major int
	Minor int
	Patch int
}

// String renders the version in the conventional major.minor.patch form.
func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Less reports whether v precedes other in release order.
func (v Version) Less(other Version) bool {
	if v.Major != other.Major {
		return v.Major < other.Major
	}
	if v.Minor != other.Minor {
		return v.Minor < other.Minor
	}
	return v.Patch < other.Patch
}

// AtLeast reports whether v is min or newer.
func (v Version) AtLeast(min Version) bool {
	return !v.Less(min)
}

// ErrVersionUnreadable means the output of `git --version` did not match the
// format git has documented and shipped for the whole lifetime of the
// project's supported range: "git version X.Y.Z ...". Getting here means the
// located binary is not behaving like git, so the platform must stop instead
// of guessing what version it is talking to.
var ErrVersionUnreadable = errors.New("gitops: could not read the git version")

const versionOutputPrefix = "git version "

// parseVersionOutput extracts a Version from the raw stdout of `git --version`.
//
// It is a total function on its input: any line that does not have the
// documented shape is ErrVersionUnreadable, never a best-effort partial
// version. Trailing platform-specific suffixes (for instance
// "2.40.0.windows.1" or "2.39.2 (Apple Git-143)") are recognized and ignored
// beyond the first three numeric components, since they are not part of the
// release number git compares against.
func parseVersionOutput(raw string) (Version, error) {
	line := strings.TrimSpace(raw)
	if idx := strings.IndexAny(line, "\r\n"); idx >= 0 {
		line = line[:idx]
	}

	if !strings.HasPrefix(line, versionOutputPrefix) {
		return Version{}, fmt.Errorf("%w: %q", ErrVersionUnreadable, raw)
	}

	fields := strings.Fields(strings.TrimPrefix(line, versionOutputPrefix))
	if len(fields) == 0 {
		return Version{}, fmt.Errorf("%w: %q", ErrVersionUnreadable, raw)
	}

	parts := strings.Split(fields[0], ".")
	if len(parts) < 2 {
		return Version{}, fmt.Errorf("%w: %q", ErrVersionUnreadable, raw)
	}

	var nums [3]int
	limit := min(len(parts), 3)
	for i := range limit {
		n, err := strconv.Atoi(parts[i])
		if err != nil || n < 0 {
			return Version{}, fmt.Errorf("%w: %q", ErrVersionUnreadable, raw)
		}
		nums[i] = n
	}

	return Version{Major: nums[0], Minor: nums[1], Patch: nums[2]}, nil
}
