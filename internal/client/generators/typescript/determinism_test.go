package typescript

import (
	"context"
	"testing"
)

func TestGenerationIsDeterministic(t *testing.T) {
	for _, f := range gateFixtures() {
		t.Run(f.Name, func(t *testing.T) {
			first, err := NewGenerator().Generate(context.Background(), f.Spec, f.Config)
			if err != nil {
				t.Fatal(err)
			}

			for i := 1; i < 12; i++ {
				next, err := NewGenerator().Generate(context.Background(), f.Spec, f.Config)
				if err != nil {
					t.Fatal(err)
				}

				if len(next.Files) != len(first.Files) {
					t.Fatalf("run %d: file count changed: %d != %d", i, len(next.Files), len(first.Files))
				}

				for name, content := range first.Files {
					if next.Files[name] != content {
						t.Fatalf("run %d: %s differs from run 0", i, name)
					}
				}
			}
		})
	}
}
