// v2/cmd/forge/plugins/client_check.go
package plugins

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/internal/client"
)

// checkUsage is rendered under USAGE: by `forge client check --help`. The exit
// codes are part of the command's contract -- this is a CI gate, and a gate
// whose codes are undocumented gets wired up wrong once and then trusted
// forever.
const checkUsage = `client check [flags]

Regenerates the client into a temporary directory using exactly the
configuration ` + "`forge client generate`" + ` would have used, and compares the result
against the committed output directory. Nothing is written to the output
directory; the temporary directory is removed on every path.

EXIT CODES:
  0  the committed client is identical to what the current spec generates
  1  drift: files differ, are missing, or are present but not generated
  2  usage or configuration error (no spec found, invalid flag, bad config)
  3  generation failed (the spec could not be parsed or the generator errored)`

// checkIgnoredDirs are directory names skipped when reading the committed
// output tree.
//
// These are not generator output: they are what a developer's tooling leaves
// behind in a package directory after `npm install` or a build. Treating them
// as drift would make check fail on every machine where anyone had ever built
// the generated client, which is every machine that uses it.
//
// The ignore is one-directional and cannot hide real drift: a path is only
// skipped on the committed side if the generator did not produce it. If the
// generator ever emits, say, dist/index.js, that path is compared like any
// other. See readCommittedTree.
var checkIgnoredDirs = map[string]bool{
	".git":         true,
	".turbo":       true,
	"node_modules": true,
	"dist":         true,
	"build":        true,
	"coverage":     true,
	".next":        true,
}

// checkIgnoredFiles are individual filenames skipped on the committed side,
// under the same one-directional rule as checkIgnoredDirs.
var checkIgnoredFiles = map[string]bool{
	".DS_Store": true,
}

// checkClient is the CI gate: regenerate, compare, exit non-zero on any
// difference. The shape of `gofmt -l`.
func (p *ClientPlugin) checkClient(ctx cli.CommandContext) error {
	base, err := p.resolveGenerationPlan(ctx)
	if err != nil {
		return err
	}

	defer base.cleanup()

	plans, err := expandClients(base)
	if err != nil {
		return err
	}

	plans, err = selectClients(plans, ctx.StringSlice("client"))
	if err != nil {
		return err
	}

	gen, err := newClientGenerator()
	if err != nil {
		return cli.WrapError(err, "check", cli.ExitInternalError)
	}

	// Every client is checked before any failure is reported, rather than
	// returning at the first drift. A gate that stops early tells you about one
	// stale client per CI run, and the next run -- after that one is
	// regenerated -- tells you about the next.
	var drifted []string

	for _, plan := range plans {
		dirty, err := p.checkOne(ctx, gen, plan)
		if err != nil {
			return err
		}

		if dirty {
			drifted = append(drifted, plan.outputDir)
		}
	}

	if len(drifted) == 0 {
		return nil
	}

	return cli.NewError(
		fmt.Sprintf(
			"client output in %s is out of date; run `forge client generate` and commit the result",
			strings.Join(drifted, ", "),
		),
		cli.ExitError,
	)
}

// checkOne regenerates a single client and compares it against what is
// committed, reporting any drift. It returns whether the client is stale;
// the error return is reserved for a check that could not be performed.
func (p *ClientPlugin) checkOne(ctx cli.CommandContext, gen *client.Generator, plan *generationPlan) (bool, error) {
	// The regenerated output goes to a temp directory, but the configuration
	// keeps its original OutputDir. Anything a generator derives from that
	// path would otherwise differ between the committed run and this one, and
	// check would report drift caused by check itself.
	tmpDir, err := os.MkdirTemp("", "forge-client-check-*")
	if err != nil {
		return false, cli.WrapError(err, "create temporary directory", cli.ExitInternalError)
	}

	defer os.RemoveAll(tmpDir)

	ctx.Info(fmt.Sprintf("Checking %s client in %s ...", plan.config.Language, plan.outputDir))

	// Parse and merge every configured source exactly as `generate` does --
	// check's whole reason to exist is that it lands on exactly the
	// configuration generate would have used, and a check that only looked at
	// the first of several configured sources would silently stop verifying
	// the rest of them.
	spec, err := resolveMergedSpec(context.Background(), plan.specPaths)
	if err != nil {
		return false, cli.WrapError(err, "parse specification", cli.ExitInternalError)
	}

	if err := applySpecTransforms(spec, plan.config); err != nil {
		return false, cli.WrapError(err, "apply spec transforms", cli.ExitInternalError)
	}

	generatedClient, err := gen.Generate(context.Background(), spec, plan.config)
	if err != nil {
		return false, cli.WrapError(err, "generate client", cli.ExitInternalError)
	}

	outputMgr := client.NewOutputManager()
	if err := outputMgr.WriteClient(generatedClient, tmpDir); err != nil {
		return false, cli.WrapError(err, "write regenerated client", cli.ExitInternalError)
	}

	for _, w := range generatedClient.Warnings {
		ctx.Warning(w)
	}

	generated, err := readGeneratedTree(tmpDir)
	if err != nil {
		return false, cli.WrapError(err, "read regenerated client", cli.ExitInternalError)
	}

	committed, err := readCommittedTree(plan.outputDir, generated)
	if err != nil {
		return false, cli.WrapError(err, "read committed client", cli.ExitInternalError)
	}

	result := compareTrees(committed, generated)
	if result.clean() {
		ctx.Success(fmt.Sprintf("Client in %s is up to date (%d files checked)", plan.outputDir, len(generated)))

		return false, nil
	}

	reportDrift(ctx, plan.outputDir, committed, generated, result)

	return true, nil
}

// treeDiff is the classification of one committed tree against one regenerated
// tree. All three lists are sorted.
type treeDiff struct {
	modified []string // present in both, contents differ
	missing  []string // the generator produces it, the committed tree does not have it
	extra    []string // the committed tree has it, the generator does not produce it
}

func (d treeDiff) clean() bool {
	return len(d.modified) == 0 && len(d.missing) == 0 && len(d.extra) == 0
}

func (d treeDiff) total() int {
	return len(d.modified) + len(d.missing) + len(d.extra)
}

// compareTrees compares file SETS as well as contents. A file that exists on
// only one side is drift just as much as one whose bytes changed -- an endpoint
// that stopped generating its own module leaves a stale file behind that still
// compiles and still exports the old surface.
func compareTrees(committed, generated map[string][]byte) treeDiff {
	var out treeDiff

	for path, want := range generated {
		got, ok := committed[path]
		if !ok {
			out.missing = append(out.missing, path)

			continue
		}

		if !bytes.Equal(got, want) {
			out.modified = append(out.modified, path)
		}
	}

	for path := range committed {
		if _, ok := generated[path]; !ok {
			out.extra = append(out.extra, path)
		}
	}

	sort.Strings(out.modified)
	sort.Strings(out.missing)
	sort.Strings(out.extra)

	return out
}

// readGeneratedTree reads every file the generator just wrote. No ignore list
// applies here: everything under this directory is generator output by
// definition.
func readGeneratedTree(root string) (map[string][]byte, error) {
	out := make(map[string][]byte)

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}

		data, readErr := os.ReadFile(path) //nolint:gosec // path comes from walking a directory this process just created
		if readErr != nil {
			return readErr
		}

		out[filepath.ToSlash(rel)] = data

		return nil
	})
	if err != nil {
		return nil, err
	}

	return out, nil
}

// readCommittedTree reads the committed output directory, skipping build
// artefacts that the generator never produces.
//
// generated is consulted so the skip can never hide a real difference: a path
// the generator produces is read even if its directory is on the ignore list.
// A missing directory is not an error -- a client that was never generated is
// the most extreme form of drift, and it is reported as every file missing
// rather than as a failure to run.
func readCommittedTree(root string, generated map[string][]byte) (map[string][]byte, error) {
	out := make(map[string][]byte)

	if _, err := os.Stat(root); os.IsNotExist(err) {
		return out, nil
	}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}

		slashRel := filepath.ToSlash(rel)

		if d.IsDir() {
			if slashRel == "." {
				return nil
			}

			if checkIgnoredDirs[d.Name()] && !generatedHasPrefix(generated, slashRel+"/") {
				return fs.SkipDir
			}

			return nil
		}

		if checkIgnoredFiles[d.Name()] {
			if _, produced := generated[slashRel]; !produced {
				return nil
			}
		}

		data, readErr := os.ReadFile(path) //nolint:gosec // path comes from walking the configured output directory
		if readErr != nil {
			return readErr
		}

		out[slashRel] = data

		return nil
	})
	if err != nil {
		return nil, err
	}

	return out, nil
}

func generatedHasPrefix(generated map[string][]byte, prefix string) bool {
	for path := range generated {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}

	return false
}

// reportDrift prints which files differ and how.
//
// Naming the files is the minimum; showing the diff is the point. Someone
// reading this in a CI log has no working copy in front of them, and "output
// differs" tells them only that they have to reproduce the run locally before
// they can start thinking.
func reportDrift(ctx cli.CommandContext, outputDir string, committed, generated map[string][]byte, result treeDiff) {
	ctx.Println("")
	ctx.Println(cli.Bold(fmt.Sprintf("%d file(s) differ between %s and a fresh generation:", result.total(), outputDir)))
	ctx.Println("")

	for _, path := range result.modified {
		ctx.Println("  M " + path + "  (contents differ)")
	}

	for _, path := range result.missing {
		ctx.Println("  + " + path + "  (generated, but not present in " + outputDir + ")")
	}

	for _, path := range result.extra {
		ctx.Println("  - " + path + "  (present in " + outputDir + ", but not generated)")
	}

	for _, path := range result.modified {
		ctx.Println("")
		ctx.Println(cli.Bold("--- " + filepath.ToSlash(filepath.Join(outputDir, path)) + " (committed)"))
		ctx.Println(cli.Bold("+++ " + path + " (regenerated)"))
		ctx.Println(renderFileDiff(committed[path], generated[path]))
	}

	for _, path := range result.missing {
		ctx.Println("")
		ctx.Println(cli.Bold("+++ " + path + " (regenerated, missing from " + outputDir + ")"))
		ctx.Println(prefixLines(string(generated[path]), "+"))
	}

	for _, path := range result.extra {
		ctx.Println("")
		ctx.Println(cli.Bold("--- " + filepath.ToSlash(filepath.Join(outputDir, path)) + " (committed, no longer generated)"))
		ctx.Println(prefixLines(string(committed[path]), "-"))
	}
}

// maxDiffLines caps a single file's rendered diff. A generated file can be
// thousands of lines, and a CI log that scrolls for ten screens is read by
// nobody.
const maxDiffLines = 200

// maxLCSCells bounds the quadratic part of the line diff. Past it, the changed
// region is rendered as a replacement block rather than line-matched -- still
// correct, just less pretty.
const maxLCSCells = 4_000_000

func renderFileDiff(oldData, newData []byte) string {
	if isBinary(oldData) || isBinary(newData) {
		return fmt.Sprintf("  (binary file differs: %d bytes committed, %d bytes regenerated)", len(oldData), len(newData))
	}

	return unifiedDiff(string(oldData), string(newData))
}

func isBinary(data []byte) bool {
	return bytes.IndexByte(data, 0) >= 0
}

func prefixLines(text, prefix string) string {
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")

	truncated := false
	if len(lines) > maxDiffLines {
		lines = lines[:maxDiffLines]
		truncated = true
	}

	var sb strings.Builder

	for _, line := range lines {
		sb.WriteString(prefix + line + "\n")
	}

	if truncated {
		sb.WriteString("  ... (truncated)\n")
	}

	return strings.TrimSuffix(sb.String(), "\n")
}

// unifiedDiff renders a line diff of two texts.
//
// Common leading and trailing lines are trimmed first, which is what makes a
// one-line change in a two-thousand-line generated file cheap to render, and
// what keeps the quadratic line matcher below off anything but the region that
// actually changed.
func unifiedDiff(oldText, newText string) string {
	oldLines := strings.Split(strings.TrimSuffix(oldText, "\n"), "\n")
	newLines := strings.Split(strings.TrimSuffix(newText, "\n"), "\n")

	prefix := 0
	for prefix < len(oldLines) && prefix < len(newLines) && oldLines[prefix] == newLines[prefix] {
		prefix++
	}

	suffix := 0
	for suffix < len(oldLines)-prefix &&
		suffix < len(newLines)-prefix &&
		oldLines[len(oldLines)-1-suffix] == newLines[len(newLines)-1-suffix] {
		suffix++
	}

	oldRegion := oldLines[prefix : len(oldLines)-suffix]
	newRegion := newLines[prefix : len(newLines)-suffix]

	const contextLines = 3

	contextBefore := oldLines[max(0, prefix-contextLines):prefix]
	contextAfter := oldLines[len(oldLines)-suffix : min(len(oldLines), len(oldLines)-suffix+contextLines)]

	var out []string

	out = append(out, fmt.Sprintf("@@ -%d,%d +%d,%d @@", prefix+1, len(oldRegion), prefix+1, len(newRegion)))

	for _, line := range contextBefore {
		out = append(out, " "+line)
	}

	out = append(out, diffRegion(oldRegion, newRegion)...)

	for _, line := range contextAfter {
		out = append(out, " "+line)
	}

	if len(out) > maxDiffLines {
		remaining := len(out) - maxDiffLines
		out = out[:maxDiffLines]
		out = append(out, fmt.Sprintf("  ... (%d more diff lines)", remaining))
	}

	return strings.Join(out, "\n")
}

// diffRegion line-matches two changed regions with an LCS, falling back to a
// plain replacement block when the region is large enough that the quadratic
// table would cost more than the result is worth.
func diffRegion(oldRegion, newRegion []string) []string {
	if len(oldRegion)*len(newRegion) > maxLCSCells {
		out := make([]string, 0, len(oldRegion)+len(newRegion))
		for _, line := range oldRegion {
			out = append(out, "-"+line)
		}

		for _, line := range newRegion {
			out = append(out, "+"+line)
		}

		return out
	}

	// Standard LCS table over lines.
	rows, cols := len(oldRegion)+1, len(newRegion)+1
	table := make([][]int, rows)

	for i := range table {
		table[i] = make([]int, cols)
	}

	for i := len(oldRegion) - 1; i >= 0; i-- {
		for j := len(newRegion) - 1; j >= 0; j-- {
			if oldRegion[i] == newRegion[j] {
				table[i][j] = table[i+1][j+1] + 1
			} else {
				table[i][j] = max(table[i+1][j], table[i][j+1])
			}
		}
	}

	var (
		out  []string
		i, j int
	)

	for i < len(oldRegion) && j < len(newRegion) {
		switch {
		case oldRegion[i] == newRegion[j]:
			out = append(out, " "+oldRegion[i])
			i++
			j++

		case table[i+1][j] >= table[i][j+1]:
			out = append(out, "-"+oldRegion[i])
			i++

		default:
			out = append(out, "+"+newRegion[j])
			j++
		}
	}

	for ; i < len(oldRegion); i++ {
		out = append(out, "-"+oldRegion[i])
	}

	for ; j < len(newRegion); j++ {
		out = append(out, "+"+newRegion[j])
	}

	return out
}
