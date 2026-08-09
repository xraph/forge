package plugins

import "testing"

func TestSourceEntriesFromScalarPath(t *testing.T) {
	s := SourceConfig{Type: "file", Path: "openapi.json"}

	got := s.Entries()

	if len(got) != 1 {
		t.Fatalf("Entries() returned %d entries, want 1", len(got))
	}
	if got[0].Path != "openapi.json" || got[0].Type != "file" {
		t.Errorf("Entries()[0] = %+v, want the scalar path as one file entry", got[0])
	}
}

func TestSourceEntriesFromScalarURL(t *testing.T) {
	s := SourceConfig{Type: "url", URL: "https://example.com/openapi.json"}

	got := s.Entries()

	if len(got) != 1 || got[0].URL != "https://example.com/openapi.json" {
		t.Fatalf("Entries() = %+v, want the scalar URL as one entry", got)
	}
}

func TestSourceEntriesPrefersExplicitList(t *testing.T) {
	s := SourceConfig{
		Type: "file",
		Path: "ignored.json",
		Sources: []SourceEntry{
			{Type: "file", Path: "openapi.json"},
			{Type: "file", Path: "asyncapi.json"},
		},
	}

	got := s.Entries()

	if len(got) != 2 {
		t.Fatalf("Entries() returned %d entries, want 2", len(got))
	}
	if got[0].Path != "openapi.json" || got[1].Path != "asyncapi.json" {
		t.Errorf("Entries() = %+v, want list order preserved", got)
	}
}

func TestSourceEntriesEmptyWhenNothingConfigured(t *testing.T) {
	if got := (SourceConfig{}).Entries(); len(got) != 0 {
		t.Errorf("Entries() = %+v, want empty", got)
	}
}
