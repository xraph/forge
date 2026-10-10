package kubernetes

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
)

func currentCondition(obj object, conditions []any, kind string) bool {
	meta, _ := obj["metadata"].(map[string]any)

	for _, entry := range conditions {
		condition, _ := entry.(map[string]any)
		if condition["type"] == kind && condition["status"] == "True" && number(condition["observedGeneration"]) >= number(meta["generation"]) {
			return true
		}
	}

	return false
}

func (k *Kubernetes) waitRoutes(ctx context.Context, d *model.Deployment) error {
	hasRoutes := false
	for _, route := range d.Routes {
		hasRoutes = hasRoutes || route.Host != ""
	}

	if !hasRoutes {
		return nil
	}

	timer := time.NewTicker(200 * time.Millisecond)
	defer timer.Stop()

	for {
		live, err := k.clusterObjects(ctx, d, true)
		if err != nil {
			return err
		}

		ready := true

		for _, route := range d.Routes {
			if route.Host == "" {
				continue
			}

			kind := "Ingress"
			if d.Target.GatewayAPI {
				kind = "HTTPRoute"
			}

			ready = ready && routeStatus(d, live[kind+"/"+route.Service+"-"+route.Port]) == state.StatusAccepted
		}

		if ready {
			return nil
		}

		select {
		case <-ctx.Done():
			return errors.New("route controller did not accept this revision before the deadline")
		case <-timer.C:
		}
	}
}
func (k *Kubernetes) routePreflight(ctx context.Context, d *model.Deployment) error {
	for _, route := range d.Routes {
		if route.Host == "" {
			continue
		}

		if route.TLS != "" {
			res, err := k.run(ctx, d, nil, "get", "secret", route.TLS, "-o", "json")
			if err != nil {
				return errors.New("route TLS Secret is unavailable")
			}

			var secret struct {
				Type string            `json:"type"`
				Data map[string]string `json:"data"`
			}
			if json.Unmarshal([]byte(res.Stdout), &secret) != nil || secret.Type != "kubernetes.io/tls" || secret.Data["tls.crt"] == "" || secret.Data["tls.key"] == "" {
				return errors.New("route TLS Secret needs a certificate and private key")
			}
		}

		if d.Target.GatewayAPI {
			res, err := k.run(ctx, d, nil, "get", "gateway", d.Target.Gateway, "-o", "json")
			if err != nil {
				return errors.New("selected Gateway is unavailable")
			}

			var gateway object
			if json.Unmarshal([]byte(res.Stdout), &gateway) != nil || gateway["kind"] != "Gateway" {
				return errors.New("selected Gateway is unavailable")
			}

			status, _ := gateway["status"].(map[string]any)

			conditions, _ := status["conditions"].([]any)
			if !currentCondition(gateway, conditions, "Programmed") {
				return errors.New("selected Gateway has not been programmed by its controller")
			}
		} else {
			if d.Target.IngressClass == "" {
				return errors.New("public routes require an explicit installed ingress_class")
			}

			res, err := k.run(ctx, d, nil, "get", "ingressclass", d.Target.IngressClass, "-o", "json")
			if err != nil {
				return errors.New("selected IngressClass is unavailable")
			}

			var ingress object
			if json.Unmarshal([]byte(res.Stdout), &ingress) != nil || ingress["kind"] != "IngressClass" {
				return errors.New("selected IngressClass is unavailable")
			}

			cfg, _ := ingress["spec"].(map[string]any)
			if controller, _ := cfg["controller"].(string); controller == "" {
				return errors.New("selected IngressClass has no controller")
			}
		}
	}

	return nil
}

// Controller acceptance is distinct from a verified public reachability probe.
func routeStatus(d *model.Deployment, obj object) state.Status {
	if obj == nil {
		return state.StatusUnknown
	}

	status, _ := obj["status"].(map[string]any)
	if !d.Target.GatewayAPI {
		lb, _ := status["loadBalancer"].(map[string]any)

		addresses, _ := lb["ingress"].([]any)
		for _, entry := range addresses {
			address, _ := entry.(map[string]any)
			if address["ip"] != nil && address["ip"] != "" || address["hostname"] != nil && address["hostname"] != "" {
				return state.StatusAccepted
			}
		}

		return state.StatusPartial
	}

	parents, _ := status["parents"].([]any)
	for _, entry := range parents {
		parent, _ := entry.(map[string]any)

		ref, _ := parent["parentRef"].(map[string]any)
		if ref["name"] != d.Target.Gateway {
			continue
		}

		conditions, _ := parent["conditions"].([]any)
		if currentCondition(obj, conditions, "Accepted") && currentCondition(obj, conditions, "ResolvedRefs") {
			return state.StatusAccepted
		}
	}

	return state.StatusPartial
}
