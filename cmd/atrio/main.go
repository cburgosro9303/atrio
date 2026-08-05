// Command atrio is the single Atrio executable: the global CLI and, once the
// portal command lands, the host of the local web portal.
//
// This main package is the one exception to the "nothing imports cli" rule
// (ADR-016) — it is the delivery layer itself, not a consumer of it. It reaches
// api only transitively through cli; importing api here is a violation. See
// internal/archtest for the enforced form of both rules.
package main

import (
	"os"

	"github.com/cburgosro9303/atrio/cli"
)

func main() {
	os.Exit(cli.Execute(os.Args, os.Stdout, os.Stderr))
}
