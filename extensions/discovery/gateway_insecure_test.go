package discovery

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// recordingTransport answers every request itself, so tests can point the
// gateway URL at any host without touching the network.
type recordingTransport struct {
	mu    sync.Mutex
	auths []string
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.auths = append(r.auths, req.Header.Get("Authorization"))
	r.mu.Unlock()

	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
}

func extensionWithRecorder(cfg FARPConfig) (*Extension, *recordingTransport) {
	rt := &recordingTransport{}
	e := &Extension{config: Config{FARP: cfg}}
	e.gatewayHTTP = &http.Client{Transport: rt}

	return e, rt
}

// The token is never put on the network in clear text unless the operator
// opts in.
func TestGatewayTokenRefusedOverPlainHTTPToRemoteHost(t *testing.T) {
	inst := &ServiceInstance{ID: "svc-1", Name: "svc", Address: "10.0.0.7", Port: 8080}

	e, rt := extensionWithRecorder(FARPConfig{GatewayURL: "http://gateway.internal:7903", GatewayToken: "s3cret"})

	if err := e.pushToGateway(t.Context(), inst); !errors.Is(err, ErrInsecureGatewayToken) {
		t.Fatalf("push returned %v, want ErrInsecureGatewayToken", err)
	}

	if err := e.deregisterFromGateway(t.Context(), inst.ID); !errors.Is(err, ErrInsecureGatewayToken) {
		t.Fatalf("deregister returned %v, want ErrInsecureGatewayToken", err)
	}

	if len(rt.auths) != 0 {
		t.Fatalf("%d requests reached the gateway", len(rt.auths))
	}

	e, rt = extensionWithRecorder(FARPConfig{
		GatewayURL: "http://gateway.internal:7903", GatewayToken: "s3cret", AllowInsecureGateway: true,
	})

	if err := e.pushToGateway(t.Context(), inst); err != nil {
		t.Fatalf("push with the opt-in failed: %v", err)
	}

	if len(rt.auths) != 1 || rt.auths[0] != "Bearer s3cret" {
		t.Fatalf("opt-in requests carried %v", rt.auths)
	}
}

func TestGatewayTransportCheck(t *testing.T) {
	cases := []struct {
		cfg     FARPConfig
		refused bool
	}{
		{FARPConfig{GatewayURL: "http://gateway.internal:7903", GatewayToken: "t"}, true},
		{FARPConfig{GatewayURL: "http://10.1.2.3", GatewayToken: "t"}, true},
		{FARPConfig{GatewayURL: "http://gateway.internal:7903", GatewayToken: "t", AllowInsecureGateway: true}, false},
		{FARPConfig{GatewayURL: "https://gateway.internal", GatewayToken: "t"}, false},
		{FARPConfig{GatewayURL: "http://localhost:7903", GatewayToken: "t"}, false},
		{FARPConfig{GatewayURL: "http://127.0.0.1:7903", GatewayToken: "t"}, false},
		{FARPConfig{GatewayURL: "http://[::1]:7903", GatewayToken: "t"}, false},
		{FARPConfig{GatewayURL: "http://gateway.internal:7903"}, false},
	}

	for _, c := range cases {
		err := c.cfg.checkGatewayTransport()
		if refused := errors.Is(err, ErrInsecureGatewayToken); refused != c.refused {
			t.Fatalf("%s (allow=%v): err=%v, want refused=%v", c.cfg.GatewayURL, c.cfg.AllowInsecureGateway, err, c.refused)
		}
	}

	cfg := DefaultConfig()
	WithFARPAllowInsecureGateway(true)(&cfg)

	if !cfg.FARP.AllowInsecureGateway {
		t.Fatal("WithFARPAllowInsecureGateway did not set the flag")
	}
}
