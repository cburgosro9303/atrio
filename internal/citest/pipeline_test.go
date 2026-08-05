package citest_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	makefileName = "Makefile"
	workflowPath = ".github/workflows/ci.yml"

	// The pair the rules below tie together: the Makefile target a developer
	// runs locally, and the workflow step that must run the same thing.
	raceTarget = "test"
	raceStep   = "Race suite"

	crossCompileStep    = "Build every platform"
	crossCompileCommand = "make build-all"
)

// TestRaceSuiteMatchesMakefile asserts that CI runs exactly the command the
// `test` target defines.
//
// The workflow cannot call `make test`: GNU make is absent from the Windows
// runner, so the step spells the command out. That duplication is the hazard —
// weaken the target locally (drop -race, drop -count=1) and CI would keep
// running the old invocation, reporting green over a gate that no longer
// exists. A comment cannot catch that; this test can.
func TestRaceSuiteMatchesMakefile(t *testing.T) {
	root := moduleRoot(t)

	makefile := readFile(t, filepath.Join(root, makefileName))
	workflow := readFile(t, filepath.Join(root, filepath.FromSlash(workflowPath)))

	want, err := makefileRecipe(makefile, raceTarget)
	if err != nil {
		t.Fatalf("reading the %q target from the %s: %v", raceTarget, makefileName, err)
	}

	got, err := workflowRunStep(workflow, raceStep)
	if err != nil {
		t.Fatalf("reading the %q step from %s: %v", raceStep, workflowPath, err)
	}

	if got != want {
		t.Errorf("the race suite drifted between the Makefile and CI\n"+
			"%s (target %q): %s\n%s (step %q): %s",
			makefileName, raceTarget, want, workflowPath, raceStep, got)
	}
}

// TestCrossCompileConsumesMakefilePlatforms asserts that CI reaches the
// cross-compile matrix through the Makefile, and never restates it.
//
// PLATFORMS is the single definition of that matrix. The rule has to be stated
// in both directions: the workflow must run `make build-all` — a rule that only
// forbade platform names would stay green if the job were deleted outright, and
// a deleted job is the loudest way to lose the coverage — and it must name no
// platform of its own, which would be a third copy of the list with nothing
// watching it.
func TestCrossCompileConsumesMakefilePlatforms(t *testing.T) {
	root := moduleRoot(t)

	makefile := readFile(t, filepath.Join(root, makefileName))
	workflow := readFile(t, filepath.Join(root, filepath.FromSlash(workflowPath)))

	command, err := workflowRunStep(workflow, crossCompileStep)
	if err != nil {
		t.Fatalf("reading the %q step from %s: %v", crossCompileStep, workflowPath, err)
	}
	if command != crossCompileCommand {
		t.Errorf("the %q step runs %q; the cross-compile matrix is reached through %q, "+
			"which is what keeps PLATFORMS in the %s its only definition",
			crossCompileStep, command, crossCompileCommand, makefileName)
	}

	platforms, err := makefilePlatforms(makefile)
	if err != nil {
		t.Fatalf("reading PLATFORMS from the %s: %v", makefileName, err)
	}

	for _, platform := range platforms {
		if strings.Contains(workflow, platform) {
			t.Errorf("%s names the platform %q; the cross-compile matrix has a single "+
				"definition, PLATFORMS in the %s, which CI reaches through `make build-all`",
				workflowPath, platform, makefileName)
		}
	}
}

// makefileRecipe returns the single command a target runs, with variable
// references expanded.
//
// It fails closed at every turn: a missing target, an empty recipe, a target
// that grew a second command, or a variable it cannot resolve all return an
// error rather than something that would compare equal by accident.
func makefileRecipe(makefile, target string) (string, error) {
	lines := strings.Split(makefile, "\n")

	start := -1
	for i, line := range lines {
		if strings.HasPrefix(line, target+":") {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return "", fmt.Errorf("no %q target found", target)
	}

	var recipe []string
	for _, line := range lines[start:] {
		// A recipe line is tab-indented; the first line that is not ends the
		// target. Blank lines and comments inside it are not commands.
		if !strings.HasPrefix(line, "\t") {
			break
		}
		command := strings.TrimSpace(strings.TrimPrefix(line, "\t"))
		if command == "" || strings.HasPrefix(command, "#") {
			continue
		}
		recipe = append(recipe, command)
	}

	switch len(recipe) {
	case 1:
		return expandMakeVariables(makefile, recipe[0])
	case 0:
		return "", fmt.Errorf("the %q target has an empty recipe", target)
	default:
		return "", fmt.Errorf("the %q target runs %d commands; this rule compares a single one",
			target, len(recipe))
	}
}

// expandMakeVariables resolves every $(VAR) in a recipe against the assignments
// declared in the Makefile.
func expandMakeVariables(makefile, command string) (string, error) {
	for {
		open := strings.Index(command, "$(")
		if open < 0 {
			return command, nil
		}
		end := strings.Index(command[open:], ")")
		if end < 0 {
			return "", fmt.Errorf("unterminated variable reference in %q", command)
		}
		end += open

		name := command[open+2 : end]
		value, err := makefileVariable(makefile, name)
		if err != nil {
			return "", err
		}
		command = command[:open] + value + command[end+1:]
	}
}

// makefileVariable returns the value assigned to a Makefile variable, accepting
// the two assignment forms this project uses.
func makefileVariable(makefile, name string) (string, error) {
	for _, line := range strings.Split(makefile, "\n") {
		for _, operator := range []string{" ?= ", " := "} {
			assignment, ok := strings.CutPrefix(line, name+operator)
			if !ok {
				continue
			}
			value := strings.TrimSpace(assignment)
			if value == "" {
				return "", fmt.Errorf("variable %q is assigned an empty value", name)
			}
			return value, nil
		}
	}
	return "", fmt.Errorf("no assignment found for variable %q", name)
}

// makefilePlatforms extracts the PLATFORMS assignment from the Makefile. It
// mirrors the helper in internal/archtest: both packages watch that list, and
// neither may import the other's test file.
func makefilePlatforms(makefile string) ([]string, error) {
	for _, line := range strings.Split(makefile, "\n") {
		rest, ok := strings.CutPrefix(line, "PLATFORMS :=")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			return nil, errors.New("PLATFORMS is declared empty in the Makefile")
		}
		return fields, nil
	}
	return nil, errors.New("no PLATFORMS assignment found in the Makefile")
}

// workflowRunStep returns the command a named workflow step runs.
//
// The workflow is read as text rather than parsed as YAML: a parser would be a
// new dependency, and every rule here needs one scalar from a step it can
// locate by name. Anything it cannot resolve unambiguously is an error, so the
// failure mode is a loud test rather than a silent pass.
func workflowRunStep(workflow, step string) (string, error) {
	lines := strings.Split(workflow, "\n")

	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "- name: "+step {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return "", fmt.Errorf("no step named %q found", step)
	}

	for _, line := range lines[start:] {
		trimmed := strings.TrimSpace(line)
		// Reaching the next step means this one carries no inline command.
		if strings.HasPrefix(trimmed, "- ") {
			break
		}
		command, ok := strings.CutPrefix(trimmed, "run: ")
		if !ok {
			continue
		}
		// A block scalar in any of its forms — |, >, and their chomping and
		// indentation variants — means the command spans lines this rule cannot
		// compare. Say so instead of comparing the indicator itself.
		if strings.HasPrefix(command, "|") || strings.HasPrefix(command, ">") {
			return "", fmt.Errorf("step %q uses a block scalar; this rule compares a "+
				"single-line command", step)
		}
		return command, nil
	}
	return "", fmt.Errorf("step %q declares no `run:` command", step)
}

// moduleRoot locates the module directory so the rules are independent of where
// this test file happens to live.
func moduleRoot(t *testing.T) string {
	t.Helper()

	// Literal arguments, passed as an array and never through a shell — the
	// same rule the gitops wrapper must follow.
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}").Output()
	if err != nil {
		t.Fatalf("locating the module root: %v", err)
	}

	root := strings.TrimSpace(string(out))
	if root == "" {
		t.Fatal("could not determine the module root: `go list -m` returned no directory")
	}
	return root
}

// readFile reads a file the rules depend on. Reading it here is also what puts
// it in the test cache key, so an edit to the Makefile or to the workflow
// re-runs these rules instead of replaying a stale pass.
func readFile(t *testing.T, path string) string {
	t.Helper()

	// The G304 exemption is narrow: every path here is a constant of this file
	// joined to the module root, with nothing from outside the process in it.
	content, err := os.ReadFile(path) //nolint:gosec // constant relative path under the module root
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(content)
}
