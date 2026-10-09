package compose

import (
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/secrets"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
)

func generatedSecrets(d *model.Deployment, st *state.Store, external map[string]string) (map[string]string, error) {
	return secrets.Generate(d, st, external)
}
func saveSecrets(st *state.Store, values map[string]string) error { return secrets.Save(st, values) }
