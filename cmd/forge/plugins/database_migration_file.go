package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"

	"github.com/xraph/forge/cli"
)

// knownGOOS and knownGOARCH hold every OS and architecture name go build reads
// out of a file name. They are copied from KnownOS and KnownArch in the Go
// 1.26 source (src/internal/syslist/syslist.go), not from "go tool dist list".
// That command only lists the ports Go supports today, while the file-name
// rule also covers retired and future names (nacl, zos, amd64p32, ...), and a
// file carrying one of those is dropped just the same. Refresh both when a
// Go release adds a GOOS or GOARCH.
var (
	knownGOOS = []string{
		"aix", "android", "darwin", "dragonfly", "freebsd", "hurd", "illumos",
		"ios", "js", "linux", "nacl", "netbsd", "openbsd", "plan9", "solaris",
		"wasip1", "windows", "zos",
	}
	knownGOARCH = []string{
		"386", "amd64", "amd64p32", "arm", "armbe", "arm64", "arm64be", "loong64",
		"mips", "mipsle", "mips64", "mips64le", "mips64p32", "mips64p32le", "ppc",
		"ppc64", "ppc64le", "riscv", "riscv64", "s390", "s390x", "sparc", "sparc64",
		"wasm",
	}
)

// Constraint kinds buildConstraintSuffix reports, worded for the error message.
const (
	constraintTest   = "a test file"
	constraintOS     = "a GOOS build constraint"
	constraintArch   = "a GOARCH build constraint"
	constraintOSArch = "a GOOS and GOARCH build constraint"
)

// buildConstraintSuffix returns the suffix that makes go build drop a
// migration file named "<version>_<name>.go", and what the toolchain takes it
// for. Both are empty when the name is safe. It follows goodOSArchFile in
// go/build, which looks at the part of the file name after the version, so a
// name that is nothing but a token ("linux") is caught too.
func buildConstraintSuffix(name string) (suffix, kind string) {
	// The version and its underscore sit in front of the name, so "test" on
	// its own makes <version>_test.go, which is a test file.
	if strings.HasSuffix("_"+name, "_test") {
		return "_test", constraintTest
	}

	// Go cuts the file name at its first dot before it reads any tags, and
	// grove's file pattern allows dots in the name.
	stem, _, _ := strings.Cut(name, ".")
	tags := strings.Split("_"+stem, "_")

	if tags[len(tags)-1] == "test" {
		tags = tags[:len(tags)-1]
	}

	last := tags[len(tags)-1]

	if len(tags) >= 2 {
		prev := tags[len(tags)-2]
		if slices.Contains(knownGOOS, prev) && slices.Contains(knownGOARCH, last) {
			return "_" + prev + "_" + last, constraintOSArch
		}
	}

	switch {
	case slices.Contains(knownGOOS, last):
		return "_" + last, constraintOS
	case slices.Contains(knownGOARCH, last):
		return "_" + last, constraintArch
	}

	return "", ""
}

// checkMigrationName rejects a name go build would silently exclude. grove's
// Group.Add takes the migration name from the file name, so a dropped file
// means a migration that never registers, and nothing reports it.
func checkMigrationName(name string) error {
	suffix, kind := buildConstraintSuffix(name)
	if suffix == "" {
		return nil
	}

	return fmt.Errorf(
		"migration name %q ends in %q, which go build treats as %s, so it would skip the file and the migration would never register: choose another name",
		name, suffix, kind,
	)
}

const (
	// groveModulePath is grove's root module, the one that holds migrate.
	groveModulePath = "github.com/xraph/grove"

	// groveDriversPrefix starts the path of every grove driver module. Each one
	// builds the schema calls a migration makes, so it has to move with grove.
	groveDriversPrefix = groveModulePath + "/drivers/"

	// groveAddVersion is the first grove release with Group.Add and the
	// migrate.DB schema builders the generated migration file uses.
	groveAddVersion = "v1.10.0"
)

// groveUpgrade is what a project's go.mod needs before an Add migration builds.
type groveUpgrade struct {
	// Current is the grove version go.mod requires today.
	Current string
	// Drivers lists the grove driver modules go.mod requires below
	// groveAddVersion, sorted. It is empty when none are required or none are old.
	Drivers []string
}

// groveUpgradeFor decides whether a go.mod holds a grove too old for the
// migration create-go writes. It returns nil when there is nothing to say:
// grove is not required, is new enough, or is replaced, so its require line no
// longer says which code gets built.
func groveUpgradeFor(goMod []byte) *groveUpgrade {
	// Not ParseLax: it skips replace directives, and the replace check below
	// depends on seeing them.
	file, err := modfile.Parse("go.mod", goMod, nil)
	if err != nil {
		return nil
	}

	replaced := make(map[string]bool, len(file.Replace))
	for _, r := range file.Replace {
		replaced[r.Old.Path] = true
	}

	var (
		current string
		drivers []string
	)

	for _, req := range file.Require {
		path, version := req.Mod.Path, req.Mod.Version

		isGrove := path == groveModulePath
		if !isGrove && !strings.HasPrefix(path, groveDriversPrefix) {
			continue
		}

		// semver.Compare ranks an invalid version below every valid one, so
		// without the IsValid check a malformed require would read as old.
		if replaced[path] || !semver.IsValid(version) || semver.Compare(version, groveAddVersion) >= 0 {
			continue
		}

		if isGrove {
			current = version
		} else {
			drivers = append(drivers, path)
		}
	}

	// Old drivers alone say nothing: the migration compiles once the grove
	// module itself is new, which is the only thing this warning promises.
	if current == "" {
		return nil
	}

	slices.Sort(drivers)

	return &groveUpgrade{Current: current, Drivers: drivers}
}

// warnIfGroveTooOld tells the user when the migration file just written will
// not compile against the grove their project requires. It never fails the
// command: a missing, unreadable, or grove-less go.mod says nothing.
func warnIfGroveTooOld(ctx cli.CommandContext, rootDir string) {
	data, err := os.ReadFile(filepath.Join(rootDir, "go.mod"))
	if err != nil {
		return
	}

	upgrade := groveUpgradeFor(data)
	if upgrade == nil {
		return
	}

	ctx.Warning(fmt.Sprintf(
		"go.mod requires %s %s, but the migration above calls App.Add, which needs %s or newer, so it will not compile yet.",
		groveModulePath, upgrade.Current, groveAddVersion,
	))

	targets := []string{groveModulePath + "@" + groveAddVersion}
	for _, driver := range upgrade.Drivers {
		targets = append(targets, driver+"@"+groveAddVersion)
	}

	fix := "   Fix it with: go get " + strings.Join(targets, " ")
	if len(upgrade.Drivers) == 0 {
		fix += " (and your grove driver modules)"
	}

	ctx.Info(fix)
	ctx.Println("")
}
