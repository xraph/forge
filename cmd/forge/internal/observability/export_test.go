package observability

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExportWritesBothAndRefusesOverwrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "obs")

	paths, err := Export(dir, DefaultRulesOptions(), false)
	if err != nil || len(paths) != 2 {
		t.Fatalf("paths %v err %v", paths, err)
	}

	for _, p := range paths {
		if info, err := os.Stat(p); err != nil || info.Size() == 0 {
			t.Fatalf("%s: %v", p, err)
		}
	}

	if _, err := Export(dir, DefaultRulesOptions(), false); !errors.Is(err, ErrExists) {
		t.Fatalf("second export err = %v", err)
	}

	if _, err := Export(dir, DefaultRulesOptions(), true); err != nil {
		t.Fatalf("forced export: %v", err)
	}
}

func TestExportValidatesBeforeWriting(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "obs")

	if _, err := Export(dir, RulesOptions{SLO: 100, P95Threshold: time.Second}, false); !errors.Is(err, ErrInvalidSLO) {
		t.Fatalf("err = %v", err)
	}

	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid options still created the output dir")
	}
}
