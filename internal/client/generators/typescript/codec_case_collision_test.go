package typescript

import (
	"context"
	"path"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

// caseCollidingSpec declares two schemas whose names differ only by case,
// each the response of its own operation. It is the shape Wakflo's backend
// hit: fs `shareResponse` next to runtime apps `ShareResponse`.
func caseCollidingSpec() *client.APISpec {
	spec := baseSpec()

	spec.Schemas["ShareResponse"] = &client.Schema{
		Type:     "object",
		Required: []string{"share_url"},
		Properties: map[string]*client.Schema{
			"share_url": {Type: "string"},
		},
	}
	spec.Schemas["shareResponse"] = &client.Schema{
		Type:     "object",
		Required: []string{"access_token"},
		Properties: map[string]*client.Schema{
			"access_token": {Type: "string"},
		},
	}

	respond := func(ref string) map[int]*client.Response {
		return map[int]*client.Response{200: {Content: map[string]*client.MediaType{
			"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/" + ref}},
		}}}
	}

	spec.Endpoints = append(spec.Endpoints,
		client.Endpoint{Method: "POST", Path: "/fs/share", OperationID: "fs.share.create", Responses: respond("shareResponse")},
		client.Endpoint{Method: "GET", Path: "/fs/share/access", OperationID: "fs.share.access", Responses: respond("shareResponse")},
		client.Endpoint{Method: "POST", Path: "/apps/share", OperationID: "apps.share", Responses: respond("ShareResponse")},
	)

	return spec
}

// relativeImport matches the specifier of every static import or re-export
// that names a path inside the generated tree.
var relativeImport = regexp.MustCompile(`(?m)^\s*(?:import|export)\b[^;]*?\bfrom\s+'(\.{1,2}/[^']+)'`)

// namedImport matches `import { a, b } from './x'`, the form every codec
// import takes, so the test can check the binding as well as the file.
var namedImport = regexp.MustCompile(`(?m)^\s*import\s+\{([^}]*)\}\s+from\s+'(\.{1,2}/[^']+)'`)

// resolveGenerated returns the generated file a relative specifier names, the
// way TypeScript's bundler resolution would, with exact case. A
// case-insensitive filesystem would resolve `./shareResponse` to
// `ShareResponse.ts`, which is the wrong codec, so a lookup that folded case
// would pass the very bug this test exists for.
func resolveGenerated(files map[string]string, from, specifier string) (string, bool) {
	target := path.Join(path.Dir(from), specifier)

	for _, candidate := range []string{target, target + ".ts", target + ".tsx", target + "/index.ts"} {
		if _, ok := files[candidate]; ok {
			return candidate, true
		}
	}

	return "", false
}

func TestCaseCollidingCodecImportsResolve(t *testing.T) {
	// Hooks on, because the per-operation modules under src/ops/ are only
	// written for a client that has hooks, and they are where the bug was.
	config := baseConfig()
	config.Hooks = true

	out, err := NewGenerator().Generate(context.Background(), caseCollidingSpec(), config)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// The precondition: the codec writer did split the two schemas into two
	// modules. If it ever stops deduplicating, this test would pass for the
	// wrong reason.
	var codecFiles []string

	for name := range out.Files {
		if strings.HasPrefix(strings.ToLower(name), "src/codecs/shareresponse") {
			codecFiles = append(codecFiles, name)
		}
	}

	sort.Strings(codecFiles)

	if len(codecFiles) != 2 {
		t.Fatalf("expected two share response codec modules, got %v", codecFiles)
	}

	names := make([]string, 0, len(out.Files))
	for name := range out.Files {
		names = append(names, name)
	}

	sort.Strings(names)

	for _, name := range names {
		if !strings.HasSuffix(name, ".ts") {
			continue
		}

		content := out.Files[name]

		for _, m := range relativeImport.FindAllStringSubmatch(content, -1) {
			if _, ok := resolveGenerated(out.Files, name, m[1]); !ok {
				t.Errorf("%s imports %q, which resolves to no generated file", name, m[1])
			}
		}

		// A codec module exports exactly one const. Importing the right file
		// under the wrong name is the other half of the same mistake.
		for _, m := range namedImport.FindAllStringSubmatch(content, -1) {
			target, ok := resolveGenerated(out.Files, name, m[2])
			if !ok || !strings.HasPrefix(target, "src/codecs/") {
				continue
			}

			for binding := range strings.SplitSeq(m[1], ",") {
				binding = strings.TrimSpace(binding)
				if binding == "" {
					continue
				}

				if !strings.Contains(out.Files[target], "export const "+binding+":") {
					t.Errorf("%s imports %s from %s, which does not export it", name, binding, target)
				}
			}
		}
	}

	// And the two operations that brought the bug to light import the
	// lower-case schema's codec, not the upper-case one's.
	for _, op := range []string{"src/ops/fs.share.create.ts", "src/ops/fs.share.access.ts"} {
		content, ok := out.Files[op]
		if !ok {
			t.Fatalf("missing %s", op)
		}

		if !strings.Contains(content, "import { codec_shareResponse } from '../codecs/shareResponse-2';") {
			t.Errorf("%s does not import the deduplicated codec module:\n%s", op, content)
		}
	}
}
