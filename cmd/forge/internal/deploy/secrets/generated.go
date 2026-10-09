package secrets

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/joho/godotenv"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
	"maps"
	"os"
	"strconv"
	"strings"
)

func Generate(d *model.Deployment, st *state.Store, external map[string]string) (map[string]string, error) {
	values := map[string]string{}

	if raw, err := st.ReadFile("generated.env"); err == nil {
		previous, err := godotenv.Unmarshal(string(raw))
		if err != nil {
			return nil, errors.New("saved deployment credentials are invalid")
		}

		maps.Copy(values, previous)
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	for _, r := range d.Resources {
		switch r.Lifecycle {
		case spec.LifecycleContainer:
			rec := r.RuntimeRecipe
			if rec == nil {
				return nil, errors.New("resource recipe missing")
			}

			prefix := EnvVarName(d.Project, r.Name)

			user, pass := values[prefix+"_USER"], values[prefix+"_PASSWORD"]
			if user == "" {
				user = "forge"
			}

			if pass == "" {
				token := make([]byte, 24)
				if _, err := rand.Read(token); err != nil {
					return nil, err
				}

				pass = hex.EncodeToString(token)
			}

			values[prefix+"_USER"], values[prefix+"_PASSWORD"] = user, pass
			values[r.Secret.EnvVar] = strings.NewReplacer("${USER}", user, "${PASSWORD}", pass, "${DATABASE}", d.Project, "${BUCKET}", r.Bucket, "{host}", r.Name, "{port}", strconv.Itoa(rec.Port)).Replace(rec.DSN)
		case spec.LifecycleExternal:
			v := external[r.Secret.EnvVar]
			if v == "" {
				return nil, fmt.Errorf("resource %s needs secret %s", r.Name, r.Secret.Name)
			}

			values[r.Secret.EnvVar] = v
		}
	}
	// Preserve generated credentials for deselected resources, without delivering unrelated process variables.
	return values, nil
}
func Save(st *state.Store, values map[string]string) error {
	raw, err := godotenv.Marshal(values)
	if err != nil {
		return errors.New("cannot encode deployment credentials")
	}

	return st.WriteFile("generated.env", []byte(strings.ReplaceAll(raw, "$", "$$")+"\n"))
}
