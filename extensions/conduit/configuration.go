package conduit

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/xraph/forge"
	adapter "github.com/xraph/forge/extensions/conduit/discovery"
	"github.com/xraph/forge/extensions/conduit/providers/jetstream"
	"github.com/xraph/forge/extensions/conduit/providers/kafka"
	"github.com/xraph/forge/extensions/conduit/providers/memory"
	"github.com/xraph/forge/extensions/conduit/providers/redisstreams"
	discoveryext "github.com/xraph/forge/extensions/discovery"
	"github.com/xraph/forge/extensions/discovery/backends"
	"gopkg.in/yaml.v3"
)

func (e *Extension) configure(app forge.App) error {
	explicit := e.runtime.Configuration()

	var cfg Config
	if err := e.LoadConfig("conduit", &cfg, nil, Config{}, false); err != nil {
		return err
	}

	mergeConfiguration(reflect.ValueOf(&cfg).Elem(), reflect.ValueOf(explicit))
	applyEnvironment(&cfg, explicit)

	if err := inferApplication(app, &cfg); err != nil {
		return err
	}

	existing := e.runtime.Providers()

	var extra []Option

	for name, connection := range cfg.Providers {
		if existing[name] != nil {
			continue
		}

		var provider Provider

		switch connection.Type {
		case "jetstream", "nats", "nats-jetstream":
			if connection.URL == "" {
				return fmt.Errorf("conduit: provider %s requires a URL", name)
			}

			provider = jetstream.New(jetstream.Options{URL: connection.URL, DeadLetterReplicas: connection.DeadLetterReplicas})
		case "redis", "redis-streams":
			if connection.URL == "" {
				return fmt.Errorf("conduit: provider %s requires a URL", name)
			}

			provider = redisstreams.New(redisstreams.Options{URL: connection.URL})
		case "kafka":
			if connection.URL == "" {
				return fmt.Errorf("conduit: provider %s requires broker addresses", name)
			}

			provider = kafka.New(kafka.Options{Brokers: strings.Split(connection.URL, ","), DeadLetterReplicas: connection.DeadLetterReplicas})
		case "memory":
			provider = memory.New()
		default:
			return fmt.Errorf("conduit: provider %s has an unsupported type", name)
		}

		extra = append(extra, WithProvider(name, provider))
		existing[name] = provider
	}

	if e.runtime.Resolver() == nil && cfg.Discovery != "none" {
		var registry Registry

		_, discoveryErr := app.GetExtension("discovery")
		hasDiscovery := discoveryErr == nil

		if manager := app.Config(); manager != nil {
			for _, key := range []string{"extensions.discovery.enabled", "discovery.enabled"} {
				if manager.IsSet(key) {
					hasDiscovery = hasDiscovery && manager.GetBool(key)

					break
				}
			}
		}

		if cfg.Discovery == "auto" {
			cfg.Discovery = ""
		}

		switch {
		case cfg.Discovery == "forge" || cfg.Discovery == "" && hasDiscovery:
			registry = adapter.NewForge(func() (backends.Backend, error) {
				service, err := forge.InjectType[*discoveryext.Service](app.Container())
				if err != nil {
					return nil, errors.New("conduit: Forge discovery is unavailable")
				}

				return service.Backend(), nil
			})
		case cfg.Discovery != "":
			candidate, ok := existing[cfg.Discovery].(Registry)
			if !ok {
				return errors.New("conduit: configured discovery provider does not implement a registry")
			}

			registry = candidate
		default:
			for _, provider := range existing {
				if candidate, ok := provider.(Registry); ok {
					if registry != nil {
						return errors.New("conduit: select a discovery provider explicitly")
					}

					registry = candidate
				}
			}
		}

		if registry != nil {
			extra = append(extra, WithRegistry(registry))
		}
	}

	return e.runtime.Configure(cfg, extra...)
}

type appManifest struct {
	App struct {
		Name      string `yaml:"name"`
		Version   string `yaml:"version"`
		Namespace string `yaml:"namespace"`
	} `yaml:"app"`
	Dev struct {
		Host string `yaml:"host"`
		Port int    `yaml:"port"`
	} `yaml:"dev"`
}

func readAppManifest() (appManifest, error) {
	var manifest appManifest

	dir, err := os.Getwd()
	if err != nil {
		return manifest, err
	}

	for range 5 {
		for _, name := range []string{".forge.yaml", ".forge.yml"} {
			path := filepath.Join(dir, name)

			info, err := os.Stat(path)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}

			if err != nil {
				return manifest, err
			}

			if !info.Mode().IsRegular() || info.Size() > 1<<20 {
				return manifest, errors.New("conduit: invalid Forge manifest file")
			}
			// #nosec G304 G703 -- Operators supply the application manifest in the application directory.
			data, err := os.ReadFile(path)
			if err != nil {
				return manifest, err
			}

			if err := yaml.Unmarshal(data, &manifest); err != nil {
				return manifest, errors.New("conduit: invalid Forge manifest YAML")
			}

			return manifest, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}

		dir = parent
	}

	return manifest, nil
}

func inferApplication(app forge.App, cfg *Config) error {
	manifest, err := readAppManifest()
	if err != nil {
		return err
	}

	if cfg.Identity.ServiceID == "" {
		cfg.Identity.ServiceID = app.Name()
		if cfg.Identity.ServiceID == "forge-app" && manifest.App.Name != "" {
			cfg.Identity.ServiceID = manifest.App.Name
		}
	}

	if cfg.Version == "" {
		cfg.Version = app.Version()
		if (cfg.Version == "1.0.0" || cfg.Version == "") && manifest.App.Version != "" {
			cfg.Version = manifest.App.Version
		}
	}

	if cfg.Identity.Namespace == "" {
		cfg.Identity.Namespace = manifest.App.Namespace
		if cfg.Identity.Namespace == "" {
			cfg.Identity.Namespace = app.Environment()
		}

		if cfg.Identity.Namespace == "" {
			cfg.Identity.Namespace = "default"
		}
	}

	if cfg.Identity.InstanceID == "" {
		cfg.Identity.InstanceID = NewID()
	}

	if len(cfg.Endpoints) != 0 {
		return nil
	}

	address := ""
	if source, ok := app.(interface{ GetHTTPAddress() string }); ok {
		address = source.GetHTTPAddress()
	}

	if address == "" {
		return nil
	}

	host, port, err := net.SplitHostPort(address)
	if err != nil {
		if _, err := strconv.Atoi(address); err != nil {
			return errors.New("conduit: cannot infer the HTTP listener address")
		}

		host, port = "", address
	}

	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return errors.New("conduit: cannot advertise an unbound HTTP port")
	}

	scheme := "http"

	if manager := app.Config(); manager != nil {
		key := "extensions.discovery.service"
		if !manager.IsSet(key) {
			key = "discovery.service"
		}

		if value := manager.GetString(key + ".address"); value != "" {
			host = value
		}

		if value := manager.GetString(key + ".metadata.scheme"); value != "" {
			scheme = value
		}
	}

	if wildcardHost(host) {
		host = os.Getenv("FORGE_ADVERTISE_ADDR")
		if wildcardHost(host) {
			host = os.Getenv("POD_IP")
		}

		if wildcardHost(host) {
			host = manifest.Dev.Host
		}

		if wildcardHost(host) {
			host, _ = os.Hostname()
		}
	}

	host = strings.Trim(host, "[]")
	if wildcardHost(host) {
		return errors.New("conduit: no reachable advertised host is available")
	}

	endpoint := Endpoint{Protocol: scheme, URL: (&url.URL{Scheme: scheme, Host: net.JoinHostPort(host, port)}).String()}
	if err := endpoint.Validate(); err != nil {
		return err
	}

	cfg.Endpoints = []Endpoint{endpoint}

	return nil
}

func wildcardHost(host string) bool {
	return host == "" || host == "0.0.0.0" || host == "::" || host == "[::]"
}

func applyEnvironment(cfg *Config, explicit Config) {
	for _, field := range []struct {
		key      string
		target   *string
		explicit string
	}{
		{"CONDUIT_NAMESPACE", &cfg.Identity.Namespace, explicit.Identity.Namespace},
		{"CONDUIT_SERVICE_ID", &cfg.Identity.ServiceID, explicit.Identity.ServiceID},
		{"CONDUIT_INSTANCE_ID", &cfg.Identity.InstanceID, explicit.Identity.InstanceID},
		{"CONDUIT_VERSION", &cfg.Version, explicit.Version},
		{"CONDUIT_DISCOVERY", &cfg.Discovery, explicit.Discovery},
	} {
		if value, ok := os.LookupEnv(field.key); ok && field.explicit == "" {
			*field.target = value
		}
	}

	for name, provider := range cfg.Providers {
		key := "CONDUIT_PROVIDERS_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_URL"
		if value, ok := os.LookupEnv(key); ok && explicit.Providers[name].URL == "" {
			provider.URL = value
			cfg.Providers[name] = provider
		}
	}
}

func mergeConfiguration(target, source reflect.Value) {
	if source.IsZero() {
		return
	}

	switch source.Kind() {
	case reflect.Struct:
		for i := range source.NumField() {
			if target.Field(i).CanSet() {
				mergeConfiguration(target.Field(i), source.Field(i))
			}
		}
	case reflect.Map:
		if target.IsNil() {
			target.Set(reflect.MakeMap(target.Type()))
		}

		for _, key := range source.MapKeys() {
			item := reflect.New(source.Type().Elem()).Elem()
			if existing := target.MapIndex(key); existing.IsValid() {
				item.Set(existing)
			}

			mergeConfiguration(item, source.MapIndex(key))
			target.SetMapIndex(key, item)
		}
	default:
		target.Set(source)
	}
}
