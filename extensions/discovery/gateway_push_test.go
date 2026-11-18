package discovery

import (
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type gatewayRecorder struct {
	mu   sync.Mutex
	reqs []recordedGatewayRequest
}

type recordedGatewayRequest struct {
	method, escapedPath, auth string
}

func (g *gatewayRecorder) handler(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	g.reqs = append(g.reqs, recordedGatewayRequest{r.Method, r.URL.EscapedPath(), r.Header.Get("Authorization")})
	g.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

// Deregistration must address the registered instance by its ID, path
// escaped, and both calls must carry the configured bearer token.
func TestGatewayPushAndDeregisterUseInstanceIDAndToken(t *testing.T) {
	rec := &gatewayRecorder{}

	srv := httptest.NewServer(http.HandlerFunc(rec.handler))
	defer srv.Close()

	e := &Extension{config: Config{FARP: FARPConfig{GatewayURL: srv.URL, GatewayToken: "s3cret"}}}
	inst := &ServiceInstance{ID: "payments/7f9c replica-2", Name: "payments", Address: "10.0.0.7", Port: 8080}
	e.serviceInstance = inst

	if err := e.pushToGateway(t.Context(), inst); err != nil {
		t.Fatal(err)
	}

	if err := e.deregisterFromGateway(t.Context(), e.registeredInstance().ID); err != nil {
		t.Fatal(err)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()

	if len(rec.reqs) != 2 {
		t.Fatalf("gateway saw %d requests, want 2", len(rec.reqs))
	}

	for _, r := range rec.reqs {
		if r.auth != "Bearer s3cret" {
			t.Fatalf("%s %s: Authorization = %q, want bearer token", r.method, r.escapedPath, r.auth)
		}
	}

	del := rec.reqs[1]
	if del.method != http.MethodDelete || del.escapedPath != "/_farp/v1/deregister/payments%2F7f9c%20replica-2" {
		t.Fatalf("deregister request = %s %s", del.method, del.escapedPath)
	}
}

func TestGatewayRequestsOmitAuthWithoutToken(t *testing.T) {
	rec := &gatewayRecorder{}

	srv := httptest.NewServer(http.HandlerFunc(rec.handler))
	defer srv.Close()

	e := &Extension{config: Config{FARP: FARPConfig{GatewayURL: srv.URL}}}
	if err := e.deregisterFromGateway(t.Context(), "id-1"); err != nil {
		t.Fatal(err)
	}

	if got := rec.reqs[0].auth; got != "" {
		t.Fatalf("Authorization = %q, want none", got)
	}
}

func TestDeregisterFromGatewayRejectsEmptyID(t *testing.T) {
	e := &Extension{config: Config{FARP: FARPConfig{GatewayURL: "http://127.0.0.1:1"}}}
	if err := e.deregisterFromGateway(t.Context(), ""); err == nil {
		t.Fatal("empty instance id accepted")
	}
}

// A gateway behind a private CA is reachable once GatewayTLS.CAFile is set.
func TestGatewayTLSUsesConfiguredCA(t *testing.T) {
	rec := &gatewayRecorder{}

	srv := httptest.NewTLSServer(http.HandlerFunc(rec.handler))
	defer srv.Close()

	// Without the CA the request must fail verification.
	plain := &Extension{config: Config{FARP: FARPConfig{GatewayURL: srv.URL}}}
	if err := plain.deregisterFromGateway(t.Context(), "id-1"); err == nil {
		t.Fatal("request to untrusted gateway succeeded")
	}

	caFile := filepath.Join(t.TempDir(), "ca.pem")

	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(caFile, caPEM, 0o600); err != nil {
		t.Fatal(err)
	}

	e := &Extension{config: Config{FARP: FARPConfig{
		GatewayURL: srv.URL,
		GatewayTLS: GatewayTLSConfig{CAFile: caFile},
	}}}
	if err := e.deregisterFromGateway(t.Context(), "id-1"); err != nil {
		t.Fatalf("request with configured CA failed: %v", err)
	}
}

func TestGatewayTLSConfigErrors(t *testing.T) {
	dir := t.TempDir()
	notPEM := filepath.Join(dir, "bad.pem")

	if err := os.WriteFile(notPEM, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := map[string]GatewayTLSConfig{
		"missing ca":     {CAFile: filepath.Join(dir, "missing.pem")},
		"ca without pem": {CAFile: notPEM},
		"cert no key":    {CertFile: notPEM},
		"key no cert":    {KeyFile: notPEM},
		"bad key pair":   {CertFile: notPEM, KeyFile: notPEM},
	}

	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			e := &Extension{config: Config{FARP: FARPConfig{GatewayURL: "https://gw", GatewayTLS: cfg}}}
			if _, err := e.gatewayClient(); err == nil || !strings.Contains(err.Error(), "farp gateway tls") {
				t.Fatalf("err = %v, want farp gateway tls error", err)
			}
		})
	}
}

func TestGatewayConfigOptions(t *testing.T) {
	cfg := DefaultConfig()
	WithFARPGatewayToken("tok")(&cfg)
	WithFARPGatewayTLS(GatewayTLSConfig{ServerName: "gw.internal"})(&cfg)

	if cfg.FARP.GatewayToken != "tok" || cfg.FARP.GatewayTLS.ServerName != "gw.internal" {
		t.Fatalf("options not applied: %+v", cfg.FARP)
	}
}
