package kubernetes

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/images"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/plan"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
)

func (k *Kubernetes) loadPlan(hash string) (*plan.Plan, error) {
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(hash) {
		return nil, errors.New("invalid recorded plan identity")
	}

	paths, err := filepath.Glob(filepath.Join(k.root, ".forge", "plans", "*-"+hash[:12]+".json"))
	if err != nil {
		return nil, err
	}

	for _, path := range paths {
		p, err := plan.Load(path)
		if err != nil {
			return nil, err
		}

		if p.Hash == hash {
			return p, nil
		}
	}

	return nil, errors.New("recorded deployment plan is unavailable")
}
func number(value any) int {
	switch n := value.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}

	return 0
}
func completed(obj object) bool {
	status, _ := obj["status"].(map[string]any)

	conditions, _ := status["conditions"].([]any)
	for _, entry := range conditions {
		condition, _ := entry.(map[string]any)
		if condition["type"] == "Complete" && condition["status"] == "True" {
			return true
		}
	}

	return false
}
func liveImage(obj object) string {
	spec, _ := obj["spec"].(map[string]any)
	template, _ := spec["template"].(map[string]any)
	pod, _ := template["spec"].(map[string]any)

	containers, _ := pod["containers"].([]any)
	if len(containers) == 0 {
		return ""
	}

	container, _ := containers[0].(map[string]any)
	image, _ := container["image"].(string)

	return image
}
func observedImage(ref string) model.Image {
	if repo, digest, ok := strings.Cut(ref, "@"); ok {
		return model.Image{Repository: repo, Digest: digest}
	}

	if index := strings.LastIndex(ref, ":"); index > strings.LastIndex(ref, "/") {
		return model.Image{Repository: ref[:index], Tag: ref[index+1:]}
	}

	return model.Image{Repository: ref}
}
func (k *Kubernetes) Observe(ctx context.Context, _ provider.EnvRef, st *state.Store) (provider.Status, error) {
	if _, err := st.Journal().Events(); err != nil {
		return provider.Status{}, err
	}

	snap, err := st.Snapshot()
	if err != nil {
		return provider.Status{}, err
	}

	p, err := k.loadPlan(snap.ActivePlanHash)
	if err != nil {
		return provider.Status{Overall: state.StatusUnknown}, err
	}

	live, err := k.clusterObjects(ctx, p.Deployment, true)
	if err != nil {
		return provider.Status{Overall: state.StatusUnknown}, err
	}

	result := provider.Status{Overall: state.StatusHealthy, Services: map[string]provider.ServiceStatus{}, Resources: map[string]state.Status{}, FailedOperation: snap.FailedOperation}
	degrade := func(status state.Status) {
		if status == state.StatusUnknown || status == state.StatusPartial {
			result.Overall = status
		} else if status == state.StatusAccepted && result.Overall == state.StatusHealthy {
			result.Overall = status
		}
	}

	workloads := snap.Workloads
	if workloads == nil {
		workloads = map[string]state.WorkloadState{}
		for _, s := range p.Deployment.Services {
			workloads[s.Name] = state.WorkloadState{PlanHash: p.Hash, Image: s.Image}
		}
	}

	for name, workload := range workloads {
		prior, err := k.loadPlan(workload.PlanHash)
		if err != nil {
			return result, err
		}

		var svc model.Service

		found := false

		for _, s := range prior.Deployment.Services {
			if s.Name == name {
				svc = s
				found = true

				break
			}
		}

		if !found {
			return result, errors.New("recorded workload is absent from its saved plan")
		}

		kind, objectName := "Deployment", name
		if svc.Kind == spec.KindJob {
			kind, objectName = "Job", jobName(prior.Deployment, name)
		}

		if svc.Kind == spec.KindCron {
			kind = "CronJob"
		}

		obj, exists := live[kind+"/"+objectName]

		ss := provider.ServiceStatus{Desired: svc.Replicas, IntendedImage: workload.Image}
		if !exists {
			ss.Message = "Recorded workload is absent"
			result.Services[name] = ss

			degrade(state.StatusUnknown)

			continue
		}

		meta, _ := obj["metadata"].(map[string]any)
		status, _ := obj["status"].(map[string]any)
		reference := liveImage(obj)

		ss.Image = observedImage(reference)
		if reference == images.Ref(workload.Image) {
			ss.Image = workload.Image
		}

		switch {
		case meta["deletionTimestamp"] != nil:
			ss.Message = "Workload is terminating"

			degrade(state.StatusPartial)
		case svc.Kind == spec.KindCron:
			ss.Message = "Schedule accepted; individual runs are observed separately"

			degrade(state.StatusAccepted)
		case svc.Kind == spec.KindJob:
			ss.Desired = 1
			if completed(obj) {
				ss.Ready = 1
				ss.Message = "Job completed"
			} else {
				ss.Message = "Job has not completed"

				degrade(state.StatusPartial)
			}
		default:
			ss.Ready = number(status["readyReplicas"])
			specObj, _ := obj["spec"].(map[string]any)

			ss.Desired = number(specObj["replicas"])
			switch {
			case number(status["observedGeneration"]) < number(meta["generation"]) || number(status["updatedReplicas"]) != ss.Desired || ss.Ready != ss.Desired || reference != images.Ref(workload.Image):
				ss.Message = "Rollout is incomplete or the image differs from the recorded release"

				degrade(state.StatusPartial)
			case svc.Health.None || svc.Health.Readiness == "":
				ss.Message = "Running without a readiness probe"

				degrade(state.StatusAccepted)
			default:
				ss.Message = "Current generation is ready"
			}
		}

		result.Services[name] = ss
	}

	for name, r := range snap.Resources {
		obj, exists := live["StatefulSet/"+name]
		status := state.StatusUnknown

		if exists {
			meta, _ := obj["metadata"].(map[string]any)

			rs, _ := obj["status"].(map[string]any)
			switch {
			case r.ProviderID != "" && meta["uid"] != r.ProviderID:
				status = state.StatusUnknown
			case number(rs["readyReplicas"]) == 1 && number(rs["observedGeneration"]) >= number(meta["generation"]) && rs["currentRevision"] == rs["updateRevision"]:
				status = state.StatusHealthy

				for _, resource := range p.Deployment.Resources {
					if resource.Name == name && resource.RuntimeRecipe != nil && len(resource.RuntimeRecipe.Healthcheck) == 0 {
						status = state.StatusAccepted
					}
				}
			default:
				status = state.StatusPartial
			}
		}

		result.Resources[name] = status
		degrade(status)
	}

	for _, r := range p.Deployment.Resources {
		if r.Lifecycle == spec.LifecycleExternal {
			result.Resources[r.Name] = state.StatusAccepted
			degrade(state.StatusAccepted)
		}
	}

	for _, r := range p.Deployment.Routes {
		if r.Host != "" {
			result.Routes = append(result.Routes, routeURL(r))
		}
	}

	if snap.FailedOperation != "" || snap.Status == state.StatusPartial || snap.Status == state.StatusCancelled {
		result.Overall = snap.Status
		if result.Overall != state.StatusCancelled {
			result.Overall = state.StatusPartial
		}
	}

	return result, nil
}
func (k *Kubernetes) Logs(ctx context.Context, ref provider.ServiceRef, opts provider.LogOptions) (io.ReadCloser, error) {
	st, err := state.Open(k.root, ref.Target, ref.Env)
	if err != nil {
		return nil, err
	}
	defer st.Close()

	snap, err := st.Snapshot()
	if err != nil {
		return nil, err
	}

	workload, ok := snap.Workloads[ref.Service]
	if !ok {
		return nil, errors.New("service has no recorded workload in this environment")
	}

	p, err := k.loadPlan(workload.PlanHash)
	if err != nil {
		return nil, err
	}

	args := []string{"logs", "--selector", selector(p.Deployment) + ",forge.xraph.io/component=" + ref.Service + ",forge.xraph.io/role=app", "--all-containers=true", "--tail=" + strconv.Itoa(max(0, opts.Tail)), "--prefix=true", "--max-log-requests=10"}
	if opts.Follow {
		args = append(args, "--follow")
	}

	streamCtx, cancel := context.WithCancel(ctx)
	read, write := io.Pipe()

	process, err := k.runner.Start(streamCtx, execx.Command{Name: "kubectl", Args: clusterArgs(p.Deployment, args...), Dir: k.root, Stdout: write})
	if err != nil {
		cancel()

		_ = read.Close()
		_ = write.Close()

		return nil, errors.New("cannot start logs for the selected workload")
	}

	go func() {
		_, err := process.Wait()
		if err != nil {
			err = errors.New("selected workload log stream ended with an error")
		}

		_ = write.CloseWithError(err)
	}()

	return &logReader{PipeReader: read, cancel: cancel}, nil
}

type logReader struct {
	*io.PipeReader

	cancel context.CancelFunc
}

func (r *logReader) Close() error {
	r.cancel()

	return r.PipeReader.Close()
}
func routeURL(r model.Route) string {
	scheme := "http"
	if r.TLS != "" {
		scheme = "https"
	}

	return scheme + "://" + r.Host + r.Path
}
