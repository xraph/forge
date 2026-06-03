package backends

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func k8sService(name, ns string, port int32, labels, annotations map[string]string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: labels, Annotations: annotations},
		Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "http", Port: port}}},
	}
}

// startTestBackend builds a KubernetesBackend over a fake clientset and waits for
// its informer cache to sync.
func startTestBackend(t *testing.T, cfg KubernetesConfig, objs ...*corev1.Service) *KubernetesBackend {
	t.Helper()
	client := fake.NewSimpleClientset()
	for _, o := range objs {
		if _, err := client.CoreV1().Services(o.Namespace).Create(context.Background(), o, metav1.CreateOptions{}); err != nil {
			t.Fatalf("seed service: %v", err)
		}
	}
	b, err := NewKubernetesBackend(cfg)
	if err != nil {
		t.Fatalf("NewKubernetesBackend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	if err := b.start(ctx, client); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}

func TestKubernetesBackend_DiscoverMapsServiceToInstance(t *testing.T) {
	svc := k8sService("twinos-identity", "twinos-platform", 8080,
		map[string]string{"app.kubernetes.io/part-of": "twinos-platform", "app.kubernetes.io/version": "1.2.3"},
		map[string]string{tagsAnnotation: "forge-dashboard-contributor, auth"})

	b := startTestBackend(t, KubernetesConfig{Namespace: "twinos-platform"}, svc)

	insts, err := b.Discover(context.Background(), "twinos-identity")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(insts) != 1 {
		t.Fatalf("want 1 instance, got %d", len(insts))
	}
	got := insts[0]
	if got.Address != "twinos-identity.twinos-platform.svc.cluster.local" {
		t.Errorf("address = %q", got.Address)
	}
	if got.Port != 8080 {
		t.Errorf("port = %d, want 8080", got.Port)
	}
	if got.Version != "1.2.3" {
		t.Errorf("version = %q, want 1.2.3", got.Version)
	}
	if got.Status != HealthStatusPassing {
		t.Errorf("status = %q", got.Status)
	}
	if !got.HasTag("forge-dashboard-contributor") || !got.HasTag("auth") {
		t.Errorf("tags = %v, want forge-dashboard-contributor + auth", got.Tags)
	}
}

func TestKubernetesBackend_ListServicesRespectsLabelScope(t *testing.T) {
	// Two platform services in the namespace; the label selector is applied at the
	// informer level, but here both match so both are listed.
	b := startTestBackend(t, KubernetesConfig{Namespace: "twinos-platform"},
		k8sService("twinos-identity", "twinos-platform", 8080, nil, nil),
		k8sService("twinos-portal", "twinos-platform", 8080, nil, nil),
	)

	names, err := b.ListServices(context.Background())
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	if len(names) != 2 || names[0] != "twinos-identity" || names[1] != "twinos-portal" {
		t.Fatalf("ListServices = %v, want sorted [twinos-identity twinos-portal]", names)
	}
}

func TestKubernetesBackend_DiscoverWithTagsFilters(t *testing.T) {
	b := startTestBackend(t, KubernetesConfig{Namespace: "ns"},
		k8sService("svc", "ns", 8080, nil, map[string]string{tagsAnnotation: "a,b"}),
	)

	if got, _ := b.DiscoverWithTags(context.Background(), "svc", []string{"a"}); len(got) != 1 {
		t.Errorf("tag 'a' should match, got %d", len(got))
	}
	if got, _ := b.DiscoverWithTags(context.Background(), "svc", []string{"missing"}); len(got) != 0 {
		t.Errorf("tag 'missing' should not match, got %d", len(got))
	}
}

func TestKubernetesBackend_RegisterIsNoop(t *testing.T) {
	b, _ := NewKubernetesBackend(KubernetesConfig{})
	if err := b.Register(context.Background(), &ServiceInstance{}); err != nil {
		t.Errorf("Register should be a no-op, got %v", err)
	}
	if err := b.Deregister(context.Background(), "x"); err != nil {
		t.Errorf("Deregister should be a no-op, got %v", err)
	}
	if b.Name() != "kubernetes" {
		t.Errorf("Name = %q", b.Name())
	}
}
