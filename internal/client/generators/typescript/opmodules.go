package typescript

import (
	"strconv"
	"strings"
)

// Operations are emitted one module per operation, rather than as rows of a
// single object literal, because a bundler cannot split an object literal.
//
// The consumer that drove this measures its budget as the source bytes of
// every module its entry statically reaches, so the only thing that moves the
// number is putting operations in separate FILES: splitting one literal into
// many `export const`s inside one file leaves that file just as reachable and
// just as heavy. Grouping by namespace was the obvious alternative and is not
// enough either -- it assumes operation ids carry a dotted prefix, and a
// document whose operationIds are bare (`getManifest`, `adminListUsers`) puts
// every operation in one bucket, which is the file we started with. One module
// per operation makes no assumption about how the id is spelled.

// opModuleNaming is the per-endpoint file/identifier assignment for the split
// operation modules, computed once so ops.ts, the per-operation modules and
// the hook modules all name the same operation the same way.
type opModuleNaming struct {
	// files[i] is the module stem under src/ops/ for endpoint i, without the
	// .ts extension. It is also the import specifier, so it must survive both
	// a filesystem and a module resolver.
	files []string
	// consts[i] is the identifier that module exports.
	consts []string
}

// newOpModuleNaming assigns a filename and an exported identifier to every
// operation key.
//
// Both are derived from the key rather than from the endpoint, so they follow
// operationKeys' own uniquification: two endpoints that collapsed onto one key
// there would collapse here too, and there they cannot.
func newOpModuleNaming(keys []string) opModuleNaming {
	return opModuleNaming{
		files:  uniqueFold(keys, opFileStem),
		consts: unique(keys, opConstName),
	}
}

// opFileStem renders one operation key as a module filename.
//
// Dots are kept rather than turned into directory separators. `agents.list`
// becomes `ops/agents.list.ts`, which both TypeScript and every bundler
// resolve from the specifier `./ops/agents.list`, and which keeps the mapping
// from an operation id to its import path a substitution a human can do in
// their head. Nesting would have to answer what happens when a document
// declares both `agents` and `agents.list`, and the answer is a directory and
// a file that differ only in extension.
//
// Anything outside the portable filename set is replaced rather than dropped,
// so two keys cannot silently converge by having their unusual characters
// deleted. A leading dot is replaced too: a key like `.internal` would
// otherwise write a hidden file that most tooling, including the drift check's
// tree walk, would simply not see.
func opFileStem(key string) string {
	if key == "" {
		return "operation"
	}

	var b strings.Builder

	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}

	stem := b.String()
	if strings.HasPrefix(stem, ".") {
		stem = "_" + stem[1:]
	}

	return guardReservedSuffix(stem)
}

// reservedFileSuffixes are trailing filename segments that tooling reads as a
// kind of file rather than as part of a name.
//
// An operation key is a dotted path and a filename is a dotted name, and the
// two conventions collide at the end of the string. `/hooks/{id}/test` derives
// the key `hooks.test`, which lands as ops/hooks.test.ts: a module every test
// runner globs as a test file, finds no tests in, and reports as a failure.
// The consumer's suite went red on three of these the first time this ran.
//
// `d` is the one that does more than annoy. ops/x.d.ts is an ambient
// declaration file, so its `export const` declares a value that no longer
// exists at runtime, and the module resolves at compile time to something the
// bundle does not contain.
var reservedFileSuffixes = map[string]bool{
	"bench": true, "config": true, "cy": true, "d": true, "e2e": true,
	"setup": true, "spec": true, "stories": true, "test": true,
}

// guardReservedSuffix joins the final segment with an underscore when tooling
// would otherwise read it as a file kind.
//
// Only the LAST segment is checked, because these conventions are suffix
// globs: `*.test.ts` matches ops/hooks.test.ts and not ops/test.hooks.ts. A
// key that is nothing but a reserved word keeps its name, since ops/test.ts is
// a module called test and matches no glob.
func guardReservedSuffix(stem string) string {
	dot := strings.LastIndex(stem, ".")
	if dot < 0 || !reservedFileSuffixes[stem[dot+1:]] {
		return stem
	}

	return stem[:dot] + "_" + stem[dot+1:]
}

// opConstName renders one operation key as the identifier its module exports.
//
// Prefixed because an operation key need not begin with a letter, and suffixed
// with nothing: uniqueness is settled by the caller, since sanitising `a.b` and
// `a_b` produces the same identifier from two keys that were distinct.
func opConstName(key string) string {
	var b strings.Builder

	b.WriteString("op_")

	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '$':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}

	return b.String()
}

// unique maps every input through render and appends a numeric suffix to any
// result that has already been produced.
//
// Deterministic because the input order is: both intermediate representation
// builders walk paths in sorted order and methods in a fixed order, which is
// the same property operationKeys relies on for its own suffixing.
func unique(in []string, render func(string) string) []string {
	out := make([]string, len(in))
	taken := make(map[string]bool, len(in))

	for i, s := range in {
		base := render(s)

		name := base
		for n := 2; taken[name]; n++ {
			name = base + strconv.Itoa(n)
		}

		taken[name] = true
		out[i] = name
	}

	return out
}

// uniqueFold is unique() with collisions judged case-insensitively.
//
// Filenames need this and identifiers do not. macOS and Windows both default
// to case-insensitive filesystems, so a document declaring `getUser` and
// `getuser` -- two distinct, legal operation ids that operationKeys is right
// to leave alone -- would emit two modules that resolve to one file, and
// whichever was written second would silently overwrite the first. The
// generated client would then compile against an operation it does not have.
//
// The fold decides collisions but never the emitted name: `getUser` keeps its
// casing and only the second arrival is suffixed, so the common case reads as
// the author wrote it.
func uniqueFold(in []string, render func(string) string) []string {
	out := make([]string, len(in))
	taken := make(map[string]bool, len(in))

	for i, s := range in {
		base := render(s)

		name := base
		for n := 2; taken[strings.ToLower(name)]; n++ {
			name = base + "-" + strconv.Itoa(n)
		}

		taken[strings.ToLower(name)] = true
		out[i] = name
	}

	return out
}

// endWithNewline trims trailing blank lines back to exactly one newline.
//
// The table writers were built to run one after another into a single file, so
// each ends with the blank line that separated it from the next. Emitted as
// files, that separator is a trailing blank line, and a generated tree that
// CI byte-diffs should not carry one.
func endWithNewline(s string) string {
	return strings.TrimRight(s, "\n") + "\n"
}
