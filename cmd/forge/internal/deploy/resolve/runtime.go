package resolve

import (
	"errors"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"gopkg.in/yaml.v3"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func runtimeConfig(in Input, s spec.Service, svc model.Service) (map[string]any, error) {
	root, err := os.OpenRoot(in.Config.RootDir)
	if err != nil {
		return nil, err
	}
	defer root.Close()

	paths := append([]string{}, s.Config...)

	if in.Discovery != nil {
		for _, app := range in.Discovery.Apps {
			if app.Name == s.App {
				paths = append(paths, app.ConfigPaths...)
			}
		}
	} else {
		paths = append(paths, filepath.Join("config", s.App+".yaml"), filepath.Join("config", s.App+".yml"), filepath.Join(svc.Dir, "config.yaml"), "config.yaml", "config.yml")
	}

	result := map[string]any{}

	for _, v := range slices.Backward(paths) {
		path := v
		if filepath.IsAbs(path) {
			path, err = filepath.Rel(in.Config.RootDir, path)
			if err != nil {
				return nil, err
			}
		}

		raw, err := root.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}

		if err != nil {
			return nil, errors.New("runtime config is not readable inside the project")
		}

		var tree map[string]any
		if yaml.Unmarshal(raw, &tree) != nil {
			return nil, errors.New("runtime config must be valid YAML")
		}

		mergeConfig(result, tree)
	}

	return result, nil
}
func mergeConfig(dst, src map[string]any) {
	for k, v := range src {
		if existing, ok := dst[k].(map[string]any); ok {
			if next, ok := v.(map[string]any); ok {
				mergeConfig(existing, next)

				continue
			}
		}

		dst[k] = v
	}
}
func containsLiteralSecret(raw []byte) bool {
	var tree any
	if yaml.Unmarshal(raw, &tree) != nil {
		return true
	}

	var check func(any, string) bool

	check = func(v any, key string) bool {
		switch x := v.(type) {
		case map[string]any:
			for k, v := range x {
				if check(v, strings.ToLower(k)) {
					return true
				}
			}
		case []any:
			for _, v := range x {
				if check(v, key) {
					return true
				}
			}
		case string:
			if x == "" || strings.Contains(x, "${") {
				return false
			}

			for _, s := range []string{"password", "secret", "token", "api_key", "access_key", "private_key"} {
				if strings.Contains(key, s) {
					return true
				}
			}

			if u, err := url.Parse(x); err == nil {
				if u.User != nil {
					if _, ok := u.User.Password(); ok {
						return true
					}
				}

				for k := range u.Query() {
					if strings.Contains(strings.ToLower(k), "password") || strings.Contains(strings.ToLower(k), "token") {
						return true
					}
				}
			}
		}

		return false
	}

	return check(tree, "")
}
