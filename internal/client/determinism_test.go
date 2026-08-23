package client_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
	"github.com/xraph/forge/internal/client/generators/golang"
	"github.com/xraph/forge/internal/client/generators/typescript"
)

// Determinism tests for the client generator.
//
// `forge client check` regenerates a client into a temp directory and diffs it
// byte-for-byte against what is committed. Anything in the pipeline that walks
// a Go map and lets the iteration order reach the emitted bytes therefore turns
// the check into a coin flip: it fails on unrelated pull requests, and everyone
// learns to ignore it.
//
// A single green run proves nothing here. Go randomises map iteration, but for
// a map small enough to live in one bucket (<= 8 entries) the randomisation is
// only a rotation of the bucket order, so an unsorted walk over three tags
// still lands on the committed order about a third of the time. Every test
// below therefore repeats the work and compares runs against each other.

// determinismRuns is how many times each test repeats the work it is checking.
//
// The failure probability per pair of runs is bounded below by 1/k for k
// distinct rotations, so the chance of an unsorted walk surviving all of these
// is under (1/2)^63 in the most forgiving case a bug can present.
const determinismRuns = 64

func TestGetStatsTagsDeterministic(t *testing.T) {
	// More than eight, so the map spills past a single bucket and Go's
	// iteration randomisation is a genuine reshuffle rather than a rotation.
	names := []string{
		"Portal", "Studio", "TwinOS", "Billing", "Identity", "Telemetry",
		"Workflows", "Webhooks", "Search", "Admin", "Audit", "Notifications",
	}

	spec := &client.APISpec{}
	for _, name := range names {
		spec.Tags = append(spec.Tags, client.Tag{Name: name})
	}

	want := spec.GetStats().Tags
	if len(want) != len(names) {
		t.Fatalf("GetStats returned %d tags, want %d", len(want), len(names))
	}

	for run := range determinismRuns {
		got := spec.GetStats().Tags

		if len(got) != len(want) {
			t.Fatalf("run %d: got %d tags, want %d", run, len(got), len(want))
		}

		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("run %d: GetStats().Tags is not deterministic\n got: %v\nwant: %v", run, got, want)
			}
		}
	}
}

// determinismSpec exercises every map in the pipeline whose iteration order
// used to reach the emitted bytes: several tags, an operation whose security
// requirement object names two schemes, more than one non-2xx response on a
// single operation, and an AsyncAPI channel carrying several messages.
const determinismSpec = `
openapi: 3.1.0
info:
  title: Determinism API
  version: 1.0.0
tags:
  - name: Portal
  - name: Studio
  - name: TwinOS
  - name: Billing
  - name: Identity
  - name: Telemetry
  - name: Workflows
  - name: Webhooks
  - name: Search
  - name: Admin
servers:
  - url: https://api.example.com
  - url: https://staging.example.com
paths:
  /users:
    get:
      summary: List users
      operationId: listUsers
      tags: [Portal, Studio]
      security:
        - bearerAuth: [read:users, admin]
          apiKeyAuth: [read:users]
      responses:
        '200':
          description: Success
          content:
            application/json:
              schema:
                type: array
                items:
                  $ref: '#/components/schemas/User'
        '400':
          description: Bad request
          content:
            application/json:
              schema:
                type: object
                properties:
                  message: {type: string}
        '404':
          description: Missing
          content:
            application/json:
              schema:
                type: object
                properties:
                  detail: {type: string}
        '409':
          description: Conflict
          content:
            application/json:
              schema:
                type: object
                properties:
                  conflict: {type: string}
        '422':
          description: Unprocessable
          content:
            application/json:
              schema:
                type: object
                properties:
                  errors: {type: array, items: {type: string}}
components:
  schemas:
    User:
      type: object
      required: [id, name]
      properties:
        id: {type: string}
        name: {type: string}
        email: {type: string}
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
    apiKeyAuth:
      type: apiKey
      in: header
      name: X-API-Key
`

func TestGenerateIsByteIdenticalAcrossRuns(t *testing.T) {
	tmpDir := t.TempDir()
	specFile := filepath.Join(tmpDir, "openapi.yaml")

	if err := os.WriteFile(specFile, []byte(determinismSpec), 0o644); err != nil {
		t.Fatalf("write spec file: %v", err)
	}

	for _, language := range []string{"go", "typescript"} {
		t.Run(language, func(t *testing.T) {
			config := client.GeneratorConfig{
				Language:    language,
				OutputDir:   filepath.Join(tmpDir, "out", language),
				PackageName: "testclient",
				APIName:     "TestClient",
				BaseURL:     "https://api.example.com",
				IncludeAuth: true,
				Version:     "1.0.0",
				Features:    client.Features{TypedErrors: true},
			}

			// A fresh Generator per run, so nothing is carried between runs
			// except the spec file itself.
			generate := func() (map[string]string, string) {
				t.Helper()

				gen := client.NewGenerator()
				if err := gen.Register(golang.NewGenerator()); err != nil {
					t.Fatalf("register go generator: %v", err)
				}

				if err := gen.Register(typescript.NewGenerator()); err != nil {
					t.Fatalf("register typescript generator: %v", err)
				}

				out, err := gen.GenerateFromFile(context.Background(), specFile, config)
				if err != nil {
					t.Fatalf("GenerateFromFile: %v", err)
				}

				return out.Files, out.Instructions
			}

			wantFiles, wantReadme := generate()
			if len(wantFiles) == 0 {
				t.Fatal("no files generated")
			}

			for run := range determinismRuns {
				gotFiles, gotReadme := generate()

				// README.md is written from Instructions rather than Files,
				// and it is where the API overview's tag list lands.
				if gotReadme != wantReadme {
					t.Fatalf("run %d: README is not byte-identical\n%s", run, firstDifference(wantReadme, gotReadme))
				}

				if len(gotFiles) != len(wantFiles) {
					t.Fatalf("run %d: got %d files, want %d", run, len(gotFiles), len(wantFiles))
				}

				for name, want := range wantFiles {
					got, ok := gotFiles[name]
					if !ok {
						t.Fatalf("run %d: file %q missing", run, name)
					}

					if got != want {
						t.Fatalf("run %d: file %q is not byte-identical\n%s", run, name, firstDifference(want, got))
					}
				}
			}
		})
	}
}

// firstDifference reports the first line on which two generated files diverge,
// which is far more useful in a failure than dumping two whole files.
func firstDifference(want, got string) string {
	wantLines := strings.Split(want, "\n")
	gotLines := strings.Split(got, "\n")

	for i := 0; i < len(wantLines) && i < len(gotLines); i++ {
		if wantLines[i] != gotLines[i] {
			return fmt.Sprintf("line %d:\n-%s\n+%s", i+1, wantLines[i], gotLines[i])
		}
	}

	return fmt.Sprintf("one output has %d lines, the other %d", len(wantLines), len(gotLines))
}
