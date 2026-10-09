package kubernetes

import (
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"strconv"
	"strings"
)

func resourcePrefix(d *model.Deployment, r model.Resource) string {
	return strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(d.Project + "_" + r.Name))
}
func recipeSubstitution(d *model.Deployment, r model.Resource) *strings.Replacer {
	return strings.NewReplacer("${USER}", "${"+resourcePrefix(d, r)+"_USER}", "${PASSWORD}", "${"+resourcePrefix(d, r)+"_PASSWORD}", "${DATABASE}", d.Project, "${BUCKET}", r.Bucket, "{host}", r.Name, "{port}", strconv.Itoa(r.RuntimeRecipe.Port))
}
func recipeEnvironment(d *model.Deployment, r model.Resource, source map[string]string) map[string]string {
	out := map[string]string{}

	replace := recipeSubstitution(d, r)
	for key, value := range source {
		out[key] = replace.Replace(value)
	}

	return out
}
func recipeProbe(d *model.Deployment, r model.Resource) object {
	command := []string{}

	replace := recipeSubstitution(d, r)
	for _, arg := range r.RuntimeRecipe.Healthcheck {
		arg = replace.Replace(arg)
		arg = strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "`", "\\`").Replace(arg)
		command = append(command, "\""+arg+"\"")
	}

	return object{"exec": object{"command": []string{"sh", "-ec", strings.Join(command, " ")}}, "periodSeconds": 5, "timeoutSeconds": 3, "failureThreshold": 6}
}
func resourceObjects(d *model.Deployment, r model.Resource) []object {
	rec := r.RuntimeRecipe

	env := recipeEnvironment(d, r, rec.Env)
	if r.Type == model.Postgres {
		env["PGDATA"] = strings.TrimSuffix(rec.Volume, "/") + "/pgdata"
	}

	uid := int64(1000)

	switch r.Type {
	case model.Postgres, model.MySQL, model.MongoDB, model.RabbitMQ:
		uid = 999
	case model.Redis:
		uid = 1001
	}

	container := object{"name": r.Name, "image": rec.Image, "env": environment(d, env), "ports": []any{object{"name": "backend", "containerPort": rec.Port, "protocol": "TCP"}}, "resources": object{"requests": object{"cpu": "100m", "memory": "128Mi"}, "limits": object{"cpu": "1", "memory": "1Gi"}}, "securityContext": object{"runAsNonRoot": true, "runAsUser": uid, "allowPrivilegeEscalation": false, "capabilities": object{"drop": []string{"ALL"}}}}
	if len(rec.Command) > 0 {
		args := []string{}

		replace := recipeSubstitution(d, r)
		for _, arg := range rec.Command {
			args = append(args, variable.ReplaceAllString(replace.Replace(arg), "$($1)"))
		}

		container["args"] = args
	}

	if len(rec.Healthcheck) > 0 {
		probe := recipeProbe(d, r)
		container["readinessProbe"] = probe
		startup := recipeProbe(d, r)
		startup["failureThreshold"] = 60
		container["startupProbe"] = startup
	}

	pod := object{"automountServiceAccountToken": false, "securityContext": object{"fsGroup": uid, "seccompProfile": object{"type": "RuntimeDefault"}}, "containers": []any{container}}
	body := object{"replicas": 1, "serviceName": r.Name, "selector": object{"matchLabels": labels(d, r.Name, "resource")}, "persistentVolumeClaimRetentionPolicy": object{"whenDeleted": "Retain", "whenScaled": "Retain"}, "template": object{"metadata": object{"labels": labels(d, r.Name, "resource")}, "spec": pod}}

	if rec.Volume != "" {
		container["volumeMounts"] = []any{object{"name": "data", "mountPath": rec.Volume}}

		size := d.Target.StorageSize
		if size == "" {
			size = "4Gi"
		}

		claim := object{"accessModes": []string{"ReadWriteOnce"}, "resources": object{"requests": object{"storage": size}}}
		if d.Target.StorageClass != "" {
			claim["storageClassName"] = d.Target.StorageClass
		}

		body["volumeClaimTemplates"] = []any{object{"metadata": object{"name": "data", "labels": labels(d, r.Name, "resource")}, "spec": claim}}
	}

	svc := serviceObject(d, r.Name, "resource", []model.Port{{Name: "backend", Port: rec.Port, Protocol: "tcp", Exposure: spec.ExposurePrivate}})
	svc["spec"].(map[string]any)["clusterIP"] = "None"
	out := []object{makeObject(d, "apps/v1", "StatefulSet", r.Name, object{"spec": body}), svc}

	if len(rec.Init) > 0 {
		image := rec.InitImage
		if image == "" {
			image = rec.Image
		}

		args := []string{}

		replace := recipeSubstitution(d, r)
		for _, arg := range rec.Init {
			args = append(args, variable.ReplaceAllString(replace.Replace(arg), "$($1)"))
		}

		init := object{"automountServiceAccountToken": false, "containers": []any{object{"name": "init", "image": image, "command": args, "env": environment(d, recipeEnvironment(d, r, rec.InitEnv)), "securityContext": object{"allowPrivilegeEscalation": false, "capabilities": object{"drop": []string{"ALL"}}}}}}
		out = append(out, makeObject(d, "batch/v1", "Job", jobName(d, r.Name+"-init"), jobBody(d, r.Name, init, "init")))
	}

	return out
}
