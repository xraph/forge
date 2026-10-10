package workbench

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/cmd/forge/internal/deploy/discover"
	"github.com/xraph/forge/cmd/forge/internal/deploy/engine"
	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
)

func importedResourcesFixture(t *testing.T) (*Server, *http.Cookie, string) {
	t.Helper()

	root := testdata.Copy(t, "bare")

	cfg, err := config.LoadForgeConfigFrom(root)
	if err != nil {
		t.Fatal(err)
	}

	runner := execx.NewFake(t)
	runner.Available["go"] = true
	runner.Script("go list -m -json all", execx.Result{})
	runner.Script("go list -deps", execx.Result{Stdout: strings.Join([]string{
		"net/http",
		"github.com/xraph/grove/drivers/mongodriver",
		"github.com/nats-io/nats.go",
		"github.com/xraph/grove/drivers/pgdriver",
		"github.com/redis/go-redis/v9",
	}, "\n")})

	e, err := engine.New(engine.Options{Config: cfg, Runner: runner})
	if err != nil {
		t.Fatal(err)
	}

	s, err := New(Options{Engine: e, Token: strings.Repeat("b", 64)})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = s.Close() })

	response := request(s, nil, "GET", s.URL(), "")
	if response.Code != http.StatusSeeOther {
		t.Fatal("token exchange failed", response.Code)
	}

	return s, response.Result().Cookies()[0], filepath.Join(root, ".forge.yml")
}

func TestInitializationExposesImportedResourceDecisions(t *testing.T) {
	s, cookie, path := importedResourcesFixture(t)

	response := request(s, cookie, "GET", "/api/project", "")
	if response.Code != http.StatusOK {
		t.Fatal(response.Code, response.Body.String())
	}

	var project struct {
		Data struct {
			Decisions []discover.Suggestion `json:"decisions"`
		} `json:"data"`
	}

	if err := json.Unmarshal(response.Body.Bytes(), &project); err != nil {
		t.Fatal(err)
	}

	if len(project.Data.Decisions) != 5 {
		t.Fatalf("expected four resource choices and Redis features: %+v", project.Data.Decisions)
	}

	for _, name := range []string{"mongodb", "nats", "postgres", "redis"} {
		index := slices.IndexFunc(project.Data.Decisions, func(d discover.Suggestion) bool {
			return d.Path == "deploy.resources."+name
		})
		if index < 0 {
			t.Fatal("missing resource choice", name)
		}

		decision := project.Data.Decisions[index]
		if decision.Kind != discover.SuggestResource || decision.Confidence != discover.Low ||
			!slices.Equal(decision.Options, []string{"include", "skip"}) || decision.Question == "" {
			t.Fatalf("resource cannot be answered: %+v", decision)
		}
	}

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	response = request(s, cookie, "POST", "/api/init", `{"answers":{}}`)

	var failed reply
	if err := json.Unmarshal(response.Body.Bytes(), &failed); err != nil {
		t.Fatal(err)
	}

	if response.Code != http.StatusBadRequest || failed.Error == nil ||
		failed.Error.Code != output.ExitUnresolved || failed.Error.Message != "decisions need answers" ||
		len(failed.Error.Diagnostics) != len(project.Data.Decisions) {
		t.Fatal("unexpected decision error", response.Code, response.Body.String())
	}

	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("unanswered decisions wrote configuration", err)
	}
}

func TestInitializationPersistsResourceAnswers(t *testing.T) {
	for _, redis := range []string{"include", "skip"} {
		t.Run(redis+" Redis", func(t *testing.T) {
			s, cookie, path := importedResourcesFixture(t)

			answers := map[string]string{
				"deploy.resources.mongodb":  "skip",
				"deploy.resources.nats":     "include",
				"deploy.resources.postgres": "include",
				"deploy.resources.redis":    redis,
			}
			if redis == "include" {
				answers["deploy.resources.redis.features"] = "none"
			}

			body, err := json.Marshal(map[string]any{"answers": answers})
			if err != nil {
				t.Fatal(err)
			}

			response := request(s, cookie, "POST", "/api/init", string(body))
			if response.Code != http.StatusOK {
				t.Fatal(response.Code, response.Body.String())
			}

			doc, _, err := spec.Parse(path)
			if err != nil || doc.Deploy == nil {
				t.Fatal("saved deployment could not load", err)
			}

			resources := doc.Deploy.Resources
			if resources["nats"].Type != "nats" || resources["postgres"].Type != "postgres" {
				t.Fatal("included resources missing", resources)
			}

			if _, ok := resources["mongodb"]; ok {
				t.Fatal("skipped MongoDB was included")
			}

			if _, ok := resources["redis"]; ok != (redis == "include") {
				t.Fatal("Redis choice was not preserved", resources)
			}
		})
	}
}
