package archtest

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// architecturePackages are the eight packages of the core decomposition, plus
// the entry point of the binary. Renaming or deleting one must trip a test.
var architecturePackages = []string{
	"api",
	"cli",
	"core",
	"flows",
	"gitops",
	"providers",
	"store",
	"web",
	"cmd/atrio",
	"internal/archtest",
}

// deliveryLayer is an entry-point package into the system, plus the exact set of
// packages allowed to depend on it. Everything not named here must not reach it
// by any route.
type deliveryLayer struct {
	name string

	// directImporters may name the layer in an import statement.
	directImporters []string

	// reachable may carry the layer in their dependency closure. It is a
	// superset of directImporters: it also covers packages that only arrive
	// through an allowed intermediary.
	reachable []string
}

// deliveryLayers encodes ADR-016, which replaced the dependency rule of ADR-012
// with two named exceptions and only two.
//
// Keeping direct imports and the transitive closure as separate checks is what
// keeps the exceptions from widening on their own: granting cli -> api would
// otherwise silently grant cmd/atrio -> api as well.
var deliveryLayers = []deliveryLayer{
	{
		// cmd/atrio is not a consumer of the delivery layer, it *is* the
		// delivery layer — which is what the rule protects downstream code from.
		name:            "cli",
		directImporters: []string{"cmd/atrio"},
		reachable:       []string{"cmd/atrio"},
	},
	{
		// The portal command lives in cli and starts the api server, the
		// direction ADR-002 already implied. cmd/atrio therefore reaches api
		// through cli, but may not import it directly.
		name:            "api",
		directImporters: []string{"cli"},
		reachable:       []string{"cli", "cmd/atrio"},
	},
}

// platform is a GOOS/GOARCH pair the rules are evaluated against.
type platform struct{ goos, goarch string }

func (p platform) String() string { return p.goos + "/" + p.goarch }

// auditPlatforms mirrors the PLATFORMS list in the Makefile.
//
// `go list` resolves build constraints for one platform at a time, so auditing
// only the host would leave a violation inside a _windows.go file invisible.
// This project will grow platform-gated files (git invocation, path handling,
// process control), which makes that a live risk rather than a hypothetical.
var auditPlatforms = []platform{
	{"linux", "amd64"},
	{"linux", "arm64"},
	{"darwin", "amd64"},
	{"darwin", "arm64"},
	{"windows", "amd64"},
}

// goPackage mirrors the subset of `go list -json` output that the rules need.
type goPackage struct {
	ImportPath string

	// Imports lists only what the package names in an import statement, which is
	// what distinguishes an allowed direct dependency from an arrival by proxy.
	Imports []string

	// Deps is the transitive closure of the production build, which is what
	// catches a forbidden dependency laundered through an intermediate package.
	Deps []string

	// TestImports and XTestImports are separate fields from Imports: a test file
	// in core importing store would go unnoticed if only Imports were checked.
	// They are direct-only, unlike Deps.
	TestImports  []string
	XTestImports []string

	Module *struct{ Path string }

	// Dir and the file lists are not used by the rules themselves. They are read
	// so that the audited sources become inputs of Go's test cache — see
	// registerSourcesAsCacheInputs.
	Dir            string
	GoFiles        []string
	TestGoFiles    []string
	XTestGoFiles   []string
	IgnoredGoFiles []string
}

// sourceFiles returns every Go file of the package, including the ones excluded
// by the current build constraints.
func (pkg goPackage) sourceFiles() []string {
	names := slices.Concat(pkg.GoFiles, pkg.TestGoFiles, pkg.XTestGoFiles, pkg.IgnoredGoFiles)
	paths := make([]string, 0, len(names))
	for _, name := range names {
		paths = append(paths, filepath.Join(pkg.Dir, name))
	}
	return paths
}

// runGo executes the go tool with an argument array — never an interpolated
// shell string, the same rule the gitops wrapper must follow — and fails the
// test if the command does not succeed.
func runGo(t *testing.T, dir string, env []string, args ...string) []byte {
	t.Helper()

	// The G204 exemption is narrow on purpose: every caller below passes literal
	// arguments, and the argument-array form is precisely what the rule asks
	// for. G204 stays enabled project-wide — it is the check that guards the
	// no-interpolated-shell rule in gitops.
	cmd := exec.Command("go", args...) //nolint:gosec // constant arguments, argument array, no shell
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("go %s failed: %v\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return stdout.Bytes()
}

// moduleRoot locates the module directory so the package listing is independent
// of where this test file happens to live.
func moduleRoot(t *testing.T) string {
	t.Helper()

	root := strings.TrimSpace(string(runGo(t, ".", nil, "list", "-m", "-f", "{{.Dir}}")))
	if root == "" {
		t.Fatal("could not determine the module root: `go list -m` returned no directory")
	}
	return root
}

// loadPackages returns the module path and every package belonging to it, as
// resolved for the given platform.
//
// It fails closed: a broken or empty listing aborts the test instead of letting
// the rules pass vacuously, which would be worse than having no rules at all.
//
// Known limitation: `go list ./...` does not surface packages under testdata
// directories, so a violation hidden there is not caught.
func loadPackages(t *testing.T, p platform) (string, []goPackage) {
	t.Helper()

	env := []string{"GOOS=" + p.goos, "GOARCH=" + p.goarch}
	out := runGo(t, moduleRoot(t), env, "list", "-json", "./...")
	dec := json.NewDecoder(bytes.NewReader(out))

	var modulePath string
	var pkgs []goPackage
	for {
		var pkg goPackage
		err := dec.Decode(&pkg)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("decoding `go list -json` output for %s: %v", p, err)
		}
		if pkg.Module == nil || pkg.Module.Path == "" {
			continue
		}
		modulePath = pkg.Module.Path
		pkgs = append(pkgs, pkg)
	}

	if modulePath == "" || len(pkgs) == 0 {
		t.Fatalf("`go list -json ./...` returned no package belonging to this module for %s", p)
	}

	registerSourcesAsCacheInputs(t, pkgs)
	return modulePath, pkgs
}

// registerSourcesAsCacheInputs reads every audited Go file so its content
// becomes part of Go's test cache key.
//
// The rules are derived entirely from a `go list` subprocess, and the cache
// cannot observe a subprocess's output — so without this, editing an audited
// file leaves the key unchanged and a cached run reports a green pass over a
// real violation. `make test` also passes -count=1; this is what protects
// anyone invoking `go test` directly, which the project's own tool allowlist
// permits.
func registerSourcesAsCacheInputs(t *testing.T, pkgs []goPackage) {
	t.Helper()

	for _, pkg := range pkgs {
		for _, path := range pkg.sourceFiles() {
			if _, err := os.ReadFile(path); err != nil { //nolint:gosec // paths come from `go list`, inside this module
				t.Fatalf("reading audited source %s: %v", path, err)
			}
		}
		// Reading the files alone only invalidates on edits. Stating the
		// directory also catches a violation that arrives as a brand-new file,
		// because adding one changes the directory's modification time.
		if _, err := os.Stat(pkg.Dir); err != nil {
			t.Fatalf("stating audited package directory %s: %v", pkg.Dir, err)
		}
	}
}

// inTree reports whether importPath is root itself or a package under it.
func inTree(importPath, root string) bool {
	return importPath == root || strings.HasPrefix(importPath, root+"/")
}

// dependencies returns everything a package depends on: the transitive
// production closure plus the direct imports of its test files.
func dependencies(pkg goPackage) []string {
	return slices.Concat(pkg.Deps, pkg.TestImports, pkg.XTestImports)
}

// directImports returns only what the package names in import statements, tests
// included, without the transitive closure.
func directImports(pkg goPackage) []string {
	return slices.Concat(pkg.Imports, pkg.TestImports, pkg.XTestImports)
}

// eachPlatform runs check against the package listing of every audited platform.
func eachPlatform(t *testing.T, check func(t *testing.T, modulePath string, pkgs []goPackage)) {
	t.Helper()

	for _, p := range auditPlatforms {
		t.Run(p.String(), func(t *testing.T) {
			modulePath, pkgs := loadPackages(t, p)
			check(t, modulePath, pkgs)
		})
	}
}

// makefilePlatforms extracts the PLATFORMS assignment from the Makefile.
func makefilePlatforms(makefile string) ([]string, error) {
	for line := range strings.SplitSeq(makefile, "\n") {
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

// TestAuditPlatformsMatchMakefile keeps the audited matrix and the
// cross-compile matrix from drifting apart. A platform added to one and not the
// other would silently lose coverage — the very failure class the matrix exists
// to close.
func TestAuditPlatformsMatchMakefile(t *testing.T) {
	makefile, err := os.ReadFile(filepath.Join(moduleRoot(t), "Makefile"))
	if err != nil {
		t.Fatalf("reading the Makefile: %v", err)
	}

	declared, err := makefilePlatforms(string(makefile))
	if err != nil {
		t.Fatal(err)
	}

	audited := make([]string, 0, len(auditPlatforms))
	for _, p := range auditPlatforms {
		audited = append(audited, p.String())
	}

	slices.Sort(declared)
	slices.Sort(audited)
	if !slices.Equal(declared, audited) {
		t.Errorf("platform matrices drifted apart\nMakefile PLATFORMS: %v\nauditPlatforms:     %v",
			declared, audited)
	}
}

// TestCoreIsPureDomain asserts that core imports no other package of this
// module, in production code and in test code alike. Its purity is what makes
// the domain exhaustively testable without I/O.
func TestCoreIsPureDomain(t *testing.T) {
	eachPlatform(t, func(t *testing.T, modulePath string, pkgs []goPackage) {
		coreRoot := modulePath + "/core"

		for _, pkg := range pkgs {
			if !inTree(pkg.ImportPath, coreRoot) {
				continue
			}
			for _, dep := range dependencies(pkg) {
				if !inTree(dep, modulePath) || inTree(dep, coreRoot) {
					continue
				}
				t.Errorf("core purity violated: %s depends on %s\n"+
					"core is the pure domain and may import no other package of this module",
					pkg.ImportPath, dep)
			}
		}
	})
}

// allowedToReach reports whether pkg is permitted to depend on the layer, given
// the list of package names the layer allows.
func allowedToReach(pkgPath, modulePath, layerRoot string, allowed []string) bool {
	// A delivery layer may always depend on itself.
	if inTree(pkgPath, layerRoot) {
		return true
	}
	for _, name := range allowed {
		if inTree(pkgPath, modulePath+"/"+name) {
			return true
		}
	}
	return false
}

// TestNothingImportsDeliveryLayers asserts that cli and api are reached only by
// the packages ADR-016 names, and by no other route.
//
// Direct imports and the transitive closure are checked against different
// allowlists on purpose: cli may import api, and cmd/atrio may therefore arrive
// at api through cli — but cmd/atrio importing api directly is still a
// violation, and only separating the two checks can tell those apart.
func TestNothingImportsDeliveryLayers(t *testing.T) {
	eachPlatform(t, func(t *testing.T, modulePath string, pkgs []goPackage) {
		for _, layer := range deliveryLayers {
			layerRoot := modulePath + "/" + layer.name

			for _, pkg := range pkgs {
				for _, dep := range directImports(pkg) {
					if !inTree(dep, layerRoot) {
						continue
					}
					if allowedToReach(pkg.ImportPath, modulePath, layerRoot, layer.directImporters) {
						continue
					}
					t.Errorf("delivery layer imported directly: %s imports %s\n"+
						"per ADR-016, only these may import %s directly: %v",
						pkg.ImportPath, dep, layer.name, layer.directImporters)
				}

				for _, dep := range pkg.Deps {
					if !inTree(dep, layerRoot) {
						continue
					}
					if allowedToReach(pkg.ImportPath, modulePath, layerRoot, layer.reachable) {
						continue
					}
					t.Errorf("delivery layer reached transitively: %s depends on %s\n"+
						"per ADR-016, only these may reach %s at all: %v",
						pkg.ImportPath, dep, layer.name, layer.reachable)
				}
			}
		}
	})
}

// TestArchitecturePackagesExist guards the decomposition itself, so that a
// rename or a deletion cannot make the rules above pass by having nothing left
// to check.
func TestArchitecturePackagesExist(t *testing.T) {
	eachPlatform(t, func(t *testing.T, modulePath string, pkgs []goPackage) {
		present := make(map[string]bool, len(pkgs))
		for _, pkg := range pkgs {
			present[pkg.ImportPath] = true
		}

		for _, name := range architecturePackages {
			if !present[modulePath+"/"+name] {
				t.Errorf("required package %s is missing from the module", name)
			}
		}
	})
}
