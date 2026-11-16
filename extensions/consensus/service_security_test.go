package consensus

import (
	"context"
	"errors"
	"testing"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// TLS settings cannot be applied by a service that opens no transport, so
// Start must refuse them instead of running without them.
func TestServiceRefusesTLSItCannotApply(t *testing.T) {
	cfg := internal.DefaultConfig()
	cfg.NodeID = "n1"
	cfg.Security.EnableTLS = true
	cfg.Security.CertFile = "cert.pem"
	cfg.Security.KeyFile = "key.pem"

	s, err := NewConsensusService(cfg, forge.NewNoopLogger(), nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Start(context.Background()); !errors.Is(err, internal.ErrInvalidConfig) {
		_ = s.Stop(context.Background())

		t.Fatalf("Start with TLS enabled returned %v", err)
	}
}
