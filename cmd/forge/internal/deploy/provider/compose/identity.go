package compose

import (
	"context"
	"errors"
	"sort"

	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
)

func (c *Compose) SnapshotIDs(ctx context.Context, d *model.Deployment) (map[string][]string, error) {
	result, err := c.run(ctx, d, "ps", "-a", "--format", "json")
	if err != nil {
		return nil, errors.New("cannot verify remote workload identities")
	}

	rows, err := parseRows(result.Stdout)
	if err != nil {
		return nil, err
	}

	ids := map[string][]string{"_forge_project": {c.projectName(d)}}

	for _, row := range rows {
		if row.ID != "" {
			ids[row.Service] = append(ids[row.Service], row.ID)
		}
	}

	for name := range ids {
		sort.Strings(ids[name])
	}

	return ids, nil
}
