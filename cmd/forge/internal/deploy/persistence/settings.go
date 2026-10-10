package persistence

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"

	"gopkg.in/yaml.v3"
)

// settingsPayload keeps only the deployment-owned section. Runtime configuration
// is merged from the checkout when the document is read or exported.
func settingsPayload(raw []byte) ([]byte, error) {
	var doc yaml.Node

	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&doc); err != nil {
		return nil, errors.New("deployment settings must be one YAML mapping")
	}

	if err := decoder.Decode(new(yaml.Node)); !errors.Is(err, io.EOF) {
		return nil, errors.New("deployment settings must be one YAML mapping")
	}

	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("deployment settings must be a mapping")
	}

	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}

	for i := 0; i+1 < len(doc.Content[0].Content); i += 2 {
		if doc.Content[0].Content[i].Value == "deploy" {
			if len(root.Content) > 0 {
				return nil, errors.New("duplicate deploy settings")
			}

			root.Content = []*yaml.Node{{Kind: yaml.ScalarNode, Tag: "!!str", Value: "deploy"}, doc.Content[0].Content[i+1]}
		}
	}

	owned := map[*yaml.Node]bool{}

	var collect func(*yaml.Node)

	collect = func(n *yaml.Node) {
		owned[n] = true
		for _, child := range n.Content {
			collect(child)
		}
	}
	collect(root)

	for n := range owned {
		if n.Kind == yaml.AliasNode && !owned[n.Alias] {
			return nil, errors.New("deploy settings cannot use file-owned YAML anchors")
		}
	}

	var out bytes.Buffer

	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)

	if err := encoder.Encode(root); err != nil {
		return nil, err
	}

	return out.Bytes(), nil
}

// scrubSettings upgrades stores that copied runtime configuration before schema 2.
func scrubSettings(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, "SELECT project,content FROM forge_deploy_settings")
	if err != nil {
		return ErrUnavailable
	}
	defer rows.Close()

	type entry struct {
		project string
		content []byte
	}

	var entries []entry

	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.project, &e.content); err != nil {
			return ErrUnavailable
		}

		entries = append(entries, e)
	}

	err = rows.Err()

	closeErr := rows.Close()
	if err != nil || closeErr != nil {
		return ErrUnavailable
	}

	for _, e := range entries {
		clean, err := settingsPayload(e.content)
		if err != nil {
			return errors.New("legacy deployment settings need a valid deploy-only YAML document")
		}

		if bytes.Equal(clean, e.content) {
			continue
		}

		if _, err := tx.ExecContext(ctx, "UPDATE forge_deploy_settings SET content=$1,revision=revision+1 WHERE project=$2", clean, e.project); err != nil {
			return ErrUnavailable
		}
	}

	return nil
}
