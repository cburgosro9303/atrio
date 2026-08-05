package gitops

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Identity is the git identity used to attribute local commits, read from
// user.name and user.email. The architecture doc (section 10) treats this as
// attribution, not authentication: git identity is self-reported and
// falsifiable, a documented and accepted limitation of the local/open-source
// model (02-registro-adr.md, "Identidad git falsificable").
type Identity struct {
	Name  string
	Email string
}

// ErrIdentityIncomplete means user.name and/or user.email are not configured
// for dir. The platform blocks on first use rather than committing with a
// missing or empty identity; the error text names exactly which command to
// run to fix it.
var ErrIdentityIncomplete = errors.New("gitops: git identity is not fully configured")

// Identity reads the effective user.name and user.email git resolves for dir
// — the same local/global/system precedence `git commit` itself would use. If
// either is missing or blank, it returns ErrIdentityIncomplete with the exact
// commands to configure what is missing.
func (b *Binary) Identity(ctx context.Context, dir string) (Identity, error) {
	name, err := b.configGet(ctx, dir, "user.name")
	if err != nil {
		return Identity{}, fmt.Errorf("gitops: reading user.name: %w", err)
	}
	email, err := b.configGet(ctx, dir, "user.email")
	if err != nil {
		return Identity{}, fmt.Errorf("gitops: reading user.email: %w", err)
	}

	haveName := name != ""
	haveEmail := email != ""

	switch {
	case haveName && haveEmail:
		return Identity{Name: name, Email: email}, nil
	case !haveName && !haveEmail:
		return Identity{}, fmt.Errorf(
			"%w: no name or email set for this repository. Configure both with:\n"+
				"  git config user.name \"Your Name\"\n"+
				"  git config user.email \"you@example.com\"\n"+
				"(add --global to either command to set it for every repository instead)",
			ErrIdentityIncomplete)
	case !haveName:
		return Identity{}, fmt.Errorf(
			"%w: user.name is not set for this repository. Configure it with:\n"+
				"  git config user.name \"Your Name\"\n"+
				"(add --global to set it for every repository instead)",
			ErrIdentityIncomplete)
	default:
		return Identity{}, fmt.Errorf(
			"%w: user.email is not set for this repository. Configure it with:\n"+
				"  git config user.email \"you@example.com\"\n"+
				"(add --global to set it for every repository instead)",
			ErrIdentityIncomplete)
	}
}

// configGet reads a single git config key for dir. An unset key is not an
// error: `git config --get` exits 1 with empty output for it, which
// configGet reports as ("", nil). Any other failure (for instance dir not
// being inside a git working tree) is returned as an error. A key whose value
// is the empty string is treated the same as an unset key, since neither is a
// usable identity component.
func (b *Binary) configGet(ctx context.Context, dir, key string) (string, error) {
	stdout, _, err := b.run(ctx, dir, "config", "--get", key)
	if err != nil {
		var rErr *runError
		if errors.As(err, &rErr) && rErr.exitCode() == 1 {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(stdout), nil
}
