package catalog

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
	"gopkg.in/yaml.v3"
)

// Load returns the embedded catalog with module-shipped and project-local
// descriptors applied on top, in that order. A descriptor with a schema
// newer than this CLI reads is reported and skipped.
func Load(ctx context.Context, projectRoot string, modules []Module) (*Catalog, output.Diagnostics, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	c := Embedded()

	var diags output.Diagnostics

	for _, m := range modules {
		if err := ctx.Err(); err != nil {
			return nil, diags, err
		}

		p := filepath.Join(m.Dir, "forge-deploy.yaml")
		if _, err := os.Stat(p); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}

			diags = append(diags, output.Diagnostic{Code: output.CodeDescriptorInvalid, Severity: output.SeverityError, Message: err.Error(), File: p})

			continue
		}

		diags = append(diags, c.applyFile(p, "module "+m.Path)...)
	}

	matches, _ := filepath.Glob(filepath.Join(projectRoot, "deploy", "descriptors", "*.yaml"))
	sort.Strings(matches)

	for _, p := range matches {
		if err := ctx.Err(); err != nil {
			return nil, diags, err
		}

		diags = append(diags, c.applyFile(p, p)...)
	}

	return c, diags, nil
}

func (c *Catalog) applyFile(path, source string) output.Diagnostics {
	data, err := os.ReadFile(path)
	if err != nil {
		return output.Diagnostics{{Code: "DEPLOY_DESCRIPTOR_INVALID", Severity: output.SeverityError, Message: err.Error(), File: path}}
	}

	var desc Descriptor

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)

	if err := decoder.Decode(&desc); err != nil {
		return output.Diagnostics{{Code: "DEPLOY_DESCRIPTOR_INVALID", Severity: output.SeverityError, Message: err.Error(), File: path}}
	}

	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return output.Diagnostics{{Code: output.CodeDescriptorInvalid, Severity: output.SeverityError, Message: "descriptor must contain exactly one document", File: path}}
	}

	if desc.Schema != descriptorSchema {
		return output.Diagnostics{{Code: "DEPLOY_DESCRIPTOR_SCHEMA", Severity: output.SeverityError,
			Message: fmt.Sprintf("descriptor %s declares schema %d; this CLI reads schema %d", desc.Extension, desc.Schema, descriptorSchema),
			File:    path, Fix: "upgrade the forge CLI"}}
	}

	if !descriptorName.MatchString(desc.Extension) || desc.ConfigKey == "" {
		return output.Diagnostics{{Code: output.CodeDescriptorInvalid, Severity: output.SeverityError, Message: "descriptor requires a valid extension name and config_key", File: path}}
	}

	known := map[model.ResourceType]bool{}
	for _, kind := range []model.ResourceType{model.Postgres, model.MySQL, model.SQLite, model.MongoDB, model.ClickHouse, model.Turso, model.Redis, model.Memcached, model.NATS, model.Kafka, model.RabbitMQ, model.ObjectStorage, model.SMTP, model.MQTT, model.Meilisearch, model.Elasticsearch, model.Typesense} {
		known[kind] = true
	}

	for _, kind := range desc.Kinds {
		if !known[kind] {
			return output.Diagnostics{{Code: output.CodeDescriptorInvalid, Severity: output.SeverityError, Message: "descriptor declares an unsupported resource kind", File: path}}
		}
	}

	for _, req := range desc.Optional {
		if !known[req.Kind] {
			return output.Diagnostics{{Code: output.CodeDescriptorInvalid, Severity: output.SeverityError, Message: "descriptor declares an unsupported optional resource kind", File: path}}
		}
	}

	desc.Source = source
	if source != "embedded" && !strings.HasPrefix(source, "module ") {
		desc.Source = path
	}

	c.Descriptors[desc.Extension] = desc

	return nil
}

var descriptorName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
