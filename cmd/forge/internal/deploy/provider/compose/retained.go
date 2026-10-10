package compose

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"

	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/plan"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
)

func (c *Compose) observedGraph(current *plan.Plan, snap state.Snapshot, st *state.Store) (*model.Deployment, error) {
	d := *current.Deployment
	d.Services = slices.Clone(d.Services)
	d.Resources = slices.Clone(d.Resources)
	services := map[string]bool{}
	resources := map[string]bool{}

	for _, service := range d.Services {
		services[service.Name] = true
	}

	for _, resource := range d.Resources {
		resources[resource.Name] = true
	}

	for name, workload := range snap.Workloads {
		if services[name] {
			continue
		}

		prior, err := c.loadPlan(st, workload.PlanHash)
		if err != nil {
			return nil, err
		}

		found := false

		for _, service := range prior.Deployment.Services {
			if service.Name == name {
				d.Services = append(d.Services, service)
				services[name] = true
				found = true
			}
		}

		if !found {
			return nil, errors.New("retained service missing from its recorded plan")
		}

		for _, resource := range prior.Deployment.Resources {
			if !resources[resource.Name] && slices.Contains(resource.UsedBy, name) {
				d.Resources = append(d.Resources, resource)
				resources[resource.Name] = true
			}
		}
	}

	slices.SortFunc(d.Services, func(a, b model.Service) int {
		if a.Name < b.Name {
			return -1
		}

		if a.Name > b.Name {
			return 1
		}

		return 0
	})

	return &d, nil
}

// observationArgs needs neither build sources nor generated secret files.
func (c *Compose) observationArgs(d *model.Deployment, st *state.Store, rest ...string) ([]string, error) {
	services := map[string]any{}
	for _, service := range d.Services {
		services[service.Name] = map[string]string{"image": "scratch"}
	}

	for _, resource := range d.Resources {
		if resource.Lifecycle == "container" {
			services[resource.Name] = map[string]string{"image": "scratch"}
		}
	}

	raw, err := json.Marshal(map[string]any{"services": services})
	if err != nil {
		return nil, err
	}

	name := "observed-compose.yml"
	if err := st.WriteFile(name, raw); err != nil {
		return nil, err
	}

	args := []string{"compose", "-p", c.projectName(d), "-f", filepath.Join(st.Dir(), name)}

	return c.dockerArgs(d, append(args, rest...)...), nil
}
