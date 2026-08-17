package golang_test

import (
	"context"
	"testing"

	"github.com/xraph/forge/internal/client/generators/golang"
)

// TestGoGeneratorIsDeterministic regenerates the same spec repeatedly and
// requires byte-identical output.
//
// Roles, permissions and scopes all originate in route metadata, which is a
// map, and Go randomizes map iteration. The sorts in CollectRoles and
// CollectPermissions are what make the output stable. Without this gate a
// regression there shows up as an unexplained diff in a downstream repo
// rather than as a failure here.
func TestGoGeneratorIsDeterministic(t *testing.T) {
	spec := specWithRolesAndPermissions()
	config := authStreamingConfig()

	first, err := golang.NewGenerator().Generate(context.Background(), spec, config)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// A single re-run can pass by luck when a map has one entry. Ten runs with
	// several roles and permissions in the fixture makes a randomized order
	// overwhelmingly likely to show up.
	for i := 0; i < 10; i++ {
		next, err := golang.NewGenerator().Generate(context.Background(), spec, config)
		if err != nil {
			t.Fatalf("Generate (run %d): %v", i, err)
		}

		if len(next.Files) != len(first.Files) {
			t.Fatalf("run %d emitted %d files, first run emitted %d",
				i, len(next.Files), len(first.Files))
		}

		for name, src := range first.Files {
			if next.Files[name] != src {
				t.Errorf("run %d: %s differs from the first run", i, name)
			}
		}
	}
}
