package plan

import (
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
	"testing"
	"time"
)

func TestMalformedPlanFailsWithoutPanic(t *testing.T) {
	for _, p := range []*Plan{nil, {Schema: Schema}, {Schema: Schema, Deployment: &model.Deployment{}}} {
		if ds := Verify(p, nil); !ds.HasErrors() {
			t.Fatal("malformed plan accepted")
		}

		if _, err := Save(t.TempDir(), p); err == nil {
			t.Fatal("malformed plan saved")
		}
	}
}
func TestFutureTimestampCannotExtendPlan(t *testing.T) {
	d, b := sample()
	inputs := map[string]string{".forge.yml": "hash"}
	p, _ := Build(d, b, state.Snapshot{}, inputs, nil)

	p.CreatedAt = time.Now().Add(48 * time.Hour)
	if ds := Verify(p, inputs); !ds.HasErrors() {
		t.Fatal("future timestamp accepted")
	}
}
