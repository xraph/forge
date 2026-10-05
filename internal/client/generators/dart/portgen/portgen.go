// Package portgen builds the Dart generator's copies of the TypeScript
// generator's language-neutral helpers.
//
// The Dart generator must name fields, key operations and decide table rows
// exactly as the TypeScript generator does, or the parity test compares two
// decisions instead of two renderings of one. So those helpers are never
// written by hand: this package copies the named declarations, doc comments
// included, applies a short list of renames and a few asserted edits, and the
// dart package's port_test fails when a committed copy differs from what
// Generate produces now. A change to the TypeScript original therefore
// surfaces as a failing test, not as silent drift.
//
// Every edit that is not a rename is asserted: if its anchor is missing or
// matches the wrong number of times, Generate returns an error rather than
// skipping the edit.
package portgen

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// client is the import line every ported file that touches the spec shares.
const client = `"github.com/xraph/forge/internal/client"`

// source names the declarations to copy out of one TypeScript file.
type source struct {
	file  string
	names []string
}

// target is one generated Dart file.
type target struct {
	file    string
	imports []string
	note    string
	sources []source
}

// edit is a replacement that is not a plain rename. It must match exactly
// count times.
type edit struct {
	old, new string
	regex    bool
	count    int
}

var targets = []target{
	{
		file:    "casing.go",
		imports: []string{`"strings"`, `"unicode"`},
		note: "// Ported verbatim from typescript/naming.go. The two generators must split\n" +
			"// and case words identically, or the client field names, and therefore the\n" +
			"// entity tables, drift apart. The parity test fails when they do.\n",
		sources: []source{{"naming.go", []string{"isAllUpper", "lowerFirst", "upperFirst", "splitWords", "toCamel", "toPascal", "toSnake"}}},
	},
	{
		file:    "opkeys.go",
		imports: []string{`"strconv"`, `"strings"`, "", client},
		note: "// Ported verbatim from typescript/opkeys.go, so a Dart operation and a\n" +
			"// TypeScript operation carry the same key.\n",
		sources: []source{{"opkeys.go", []string{"operationKeys", "endpointKey", "operationIDFromPath"}}},
	},
	{
		file:    "fieldname.go",
		imports: []string{`"fmt"`, `"strings"`, "", client},
		note: "// Ported from typescript/fieldname.go with tsFieldName renamed\n" +
			"// clientFieldName and Dart added to the camel default.\n",
		sources: []source{{"fieldname.go", []string{
			"tsFieldName", "effectiveFieldNaming", "checkFieldNameCollisions", "dedupeMessages",
			"maxFieldCollisionDepth", "checkSchemaFieldCollisions", "checkFlattenedAllOfCollisions",
		}}},
	},
	{
		file:    "codectable.go",
		imports: []string{`"fmt"`, `"sort"`, `"strings"`, "", client},
		note: "// Ported from typescript/codecs.go (the table builder only, not the\n" +
			"// TypeScript renderer) with tsFieldName renamed clientFieldName and the\n" +
			"// codecField.TS field renamed Client. The Dart renderer is codecs.go.\n",
		sources: []source{
			{"codecs.go", []string{
				"codecEntry", "codecField", "codecDiscriminator", "codecTable", "refName", "arrayRefCodecID",
				"registerEndpointArrayBodyCodecs", "additionalPropertiesSegment", "codecIDFor", "add",
				"unionEntry", "checkDiscriminatorTSNameAgreement", "allOfLayer", "flattenAllOfLayers",
				"allOfEntry", "requiredWireFields", "buildCodecTable", "sortedCodecIDs",
			}},
			{"generator.go", []string{"additionalPropsSchema"}},
		},
	},
	{
		file:    "tagrename.go",
		imports: []string{`"regexp"`, `"strings"`, "", client},
		note: "// Ported verbatim from typescript/tagrename.go with tsFieldName renamed\n" +
			"// clientFieldName.\n",
		sources: []source{{"tagrename.go", []string{
			"placeholderPattern", "renameDeclaredTags", "renamePlaceholder", "renameSegments", "descend",
			"rootOf", "requestBodyRoot", "responseRoot", "schemaProperties", "isParameterName", "isIndex",
		}}},
	},
	{
		file:    "manifest.go",
		imports: []string{`"fmt"`, `"sort"`, `"strings"`, "", client},
		note: "// Ported verbatim from typescript/opsmanifest.go, typescript/rest.go,\n" +
			"// typescript/facades.go and typescript/capabilities.go: the helpers that\n" +
			"// decide what a table row says. Kept identical so the parity test compares\n" +
			"// two renderings of one decision, not two decisions.\n",
		sources: []source{
			{"opsmanifest.go", []string{
				"entityRow", "entityRows", "renameEntityField", "renameEntityFields", "renameDerivedIDTags",
				"operationSecurityKeys", "duplexMessageNames",
			}},
			{"rest.go", []string{"requestBodyContentType", "endpointLabel", "schemaCodecRef", "requestBodyCodecRef", "responseCodecRef"}},
			{"facades.go", []string{"isReadMethod"}},
			{"capabilities.go", []string{"sortedUniqueStrings"}},
		},
	},
}

// renames are applied to every ported file, in order.
var renames = [][2]string{
	{"tsFieldName", "clientFieldName"},
	{"checkDiscriminatorTSNameAgreement", "checkDiscriminatorNameAgreement"},
	// The originals' comments use a few dashes the house style avoids.
	{"\u2014", "--"},
	{"\u2013", "-"},
}

// edits are the changes that are not renames, per target file.
var edits = map[string][]edit{
	"codectable.go": {
		{old: "TS    string `json:\"ts\"`", new: "Client string `json:\"client\"`", count: 1},
		{old: "field.TS", new: "field.Client", count: 1},
		{old: `\bTS:\s*clientFieldName`, new: "Client: clientFieldName", regex: true, count: 2},
	},
	"fieldname.go": {
		{
			old:   "\tif config.Language == \"typescript\" {",
			new:   "\tif config.Language == \"typescript\" || config.Language == \"dart\" {",
			count: 1,
		},
		{
			old: "func checkFieldNameCollisions(spec *client.APISpec, config client.GeneratorConfig) error {\n" +
				"\tif !codecsNeeded(config) {\n\t\treturn nil\n\t}\n\n",
			new: "//\n" +
				"// Unlike the TypeScript original this always runs: a Dart model declares one\n" +
				"// member per client name, so two wire names landing on one client name is a\n" +
				"// duplicate member whatever the naming strategy.\n" +
				"func checkFieldNameCollisions(spec *client.APISpec, config client.GeneratorConfig) error {\n",
			count: 1,
		},
	},
}

// Generate builds every ported file from the TypeScript sources in tsDir and
// returns the formatted contents keyed by file name.
func Generate(tsDir string) (map[string]string, error) {
	parsed := map[string]map[string]string{}

	out := make(map[string]string, len(targets))

	for _, t := range targets {
		var body []string

		for _, src := range t.sources {
			decls, ok := parsed[src.file]
			if !ok {
				var err error

				decls, err = declarations(filepath.Join(tsDir, src.file))
				if err != nil {
					return nil, err
				}

				parsed[src.file] = decls
			}

			for _, name := range src.names {
				text, found := decls[name]
				if !found {
					return nil, fmt.Errorf("%s: declaration %q not found in typescript/%s", t.file, name, src.file)
				}

				body = append(body, text+"\n")
			}
		}

		var b strings.Builder

		b.WriteString("package dart\n\nimport (\n")

		for _, imp := range t.imports {
			if imp == "" {
				b.WriteString("\n")

				continue
			}

			b.WriteString("\t" + imp + "\n")
		}

		b.WriteString(")\n\n" + t.note + "\n" + strings.Join(body, "\n"))

		text := b.String()
		for _, r := range renames {
			text = strings.ReplaceAll(text, r[0], r[1])
		}

		var err error

		for _, e := range edits[t.file] {
			if text, err = apply(t.file, text, e); err != nil {
				return nil, err
			}
		}

		formatted, err := format.Source([]byte(text))
		if err != nil {
			return nil, fmt.Errorf("%s: ported source does not parse: %w", t.file, err)
		}

		out[t.file] = string(formatted)
	}

	return out, nil
}

// Write generates the ported files and writes them into dartDir.
func Write(tsDir, dartDir string) error {
	files, err := Generate(tsDir)
	if err != nil {
		return err
	}

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}

	sort.Strings(names)

	for _, name := range names {
		//nolint:gosec // generated source files are world-readable like the rest of the tree
		err := os.WriteFile(filepath.Join(dartDir, name), []byte(files[name]), 0o644)
		if err != nil {
			return err
		}
	}

	return nil
}

func apply(file, text string, e edit) (string, error) {
	if e.regex {
		re := regexp.MustCompile(e.old)
		if n := len(re.FindAllStringIndex(text, -1)); n != e.count {
			return "", fmt.Errorf("%s: edit %q matched %d times, want %d", file, e.old, n, e.count)
		}

		return re.ReplaceAllString(text, e.new), nil
	}

	if n := strings.Count(text, e.old); n != e.count {
		return "", fmt.Errorf("%s: edit anchor %q matched %d times, want %d", file, e.old, n, e.count)
	}

	return strings.ReplaceAll(text, e.old, e.new), nil
}

// declarations returns the source text of every top-level declaration in path,
// doc comment included, keyed by the declared name. Methods are keyed by their
// bare method name; a name declared twice in one file is an error, so a lookup
// can never silently pick the wrong one.
func declarations(path string) (map[string]string, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	out := map[string]string{}

	for _, d := range file.Decls {
		var (
			name string
			doc  *ast.CommentGroup
		)

		switch d := d.(type) {
		case *ast.FuncDecl:
			name, doc = d.Name.Name, d.Doc
		case *ast.GenDecl:
			if d.Tok == token.IMPORT || len(d.Specs) == 0 {
				continue
			}

			doc = d.Doc

			switch s := d.Specs[0].(type) {
			case *ast.TypeSpec:
				name = s.Name.Name
			case *ast.ValueSpec:
				name = s.Names[0].Name
			}
		}

		start := d.Pos()
		if doc != nil {
			start = doc.Pos()
		}

		if _, dup := out[name]; dup {
			return nil, fmt.Errorf("%s: %q is declared more than once", path, name)
		}

		out[name] = string(src[fset.Position(start).Offset:fset.Position(d.End()).Offset])
	}

	return out, nil
}
