package core

import "fmt"

// ValidateClosureEnvironment checks that closureEnvironment names one of the
// project's declared environments. The two values live in different
// artifacts — a task and the project configuration — so this stays a pure
// function of both, taken as plain arguments: core performs no I/O to fetch
// either one itself (schemas/README.md: "closureEnvironment ∈ environments
// del proyecto").
func ValidateClosureEnvironment(closureEnvironment string, environments []string) error {
	for _, env := range environments {
		if env == closureEnvironment {
			return nil
		}
	}
	return fmt.Errorf(
		"core: closure environment %q is not one of the project's declared environments %v",
		closureEnvironment, environments,
	)
}
