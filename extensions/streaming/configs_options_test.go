package streaming

import (
	"testing"
	"time"
)

// TestConfigOptions_SessionResumptionAndLoadBalancer pins D1: both features were
// reachable only by hand-building a Config because configs.go never re-exported
// their options.
func TestConfigOptions_SessionResumptionAndLoadBalancer(t *testing.T) {
	tests := []struct {
		name   string
		option ConfigOption
		check  func(*testing.T, Config)
	}{
		{
			name:   "WithSessionResumption enables resumption and sets the TTL",
			option: WithSessionResumption(90 * time.Second),
			check: func(t *testing.T, c Config) {
				if !c.EnableSessionResumption {
					t.Error("EnableSessionResumption = false, want true")
				}

				if c.SessionResumptionTTL != 90*time.Second {
					t.Errorf("SessionResumptionTTL = %v, want %v", c.SessionResumptionTTL, 90*time.Second)
				}
			},
		},
		{
			name:   "WithSessionResumption keeps the default TTL when given zero",
			option: WithSessionResumption(0),
			check: func(t *testing.T, c Config) {
				if !c.EnableSessionResumption {
					t.Error("EnableSessionResumption = false, want true")
				}

				if c.SessionResumptionTTL != 30*time.Second {
					t.Errorf("SessionResumptionTTL = %v, want the default %v", c.SessionResumptionTTL, 30*time.Second)
				}
			},
		},
		{
			name:   "WithLoadBalancer enables balancing and sets the strategy",
			option: WithLoadBalancer("consistent_hash"),
			check: func(t *testing.T, c Config) {
				if !c.EnableLoadBalancer {
					t.Error("EnableLoadBalancer = false, want true")
				}

				if c.LoadBalancerStrategy != "consistent_hash" {
					t.Errorf("LoadBalancerStrategy = %q, want %q", c.LoadBalancerStrategy, "consistent_hash")
				}
			},
		},
		{
			name:   "WithLoadBalancer keeps the default strategy when given an empty string",
			option: WithLoadBalancer(""),
			check: func(t *testing.T, c Config) {
				if !c.EnableLoadBalancer {
					t.Error("EnableLoadBalancer = false, want true")
				}

				if c.LoadBalancerStrategy != "round_robin" {
					t.Errorf("LoadBalancerStrategy = %q, want the default %q", c.LoadBalancerStrategy, "round_robin")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.option(&cfg)
			tt.check(t, cfg)
		})
	}
}

func TestConfigOptions_DefaultsLeaveBothFeaturesOff(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.EnableSessionResumption {
		t.Error("EnableSessionResumption = true by default, want false")
	}

	if cfg.EnableLoadBalancer {
		t.Error("EnableLoadBalancer = true by default, want false")
	}
}

// TestConfigOptions_ComposeWithOtherOptions checks the re-exported options behave
// like the rest of the option set when applied together.
func TestConfigOptions_ComposeWithOtherOptions(t *testing.T) {
	cfg := DefaultConfig()

	for _, opt := range []ConfigOption{
		WithLocalBackend(),
		WithSessionResumption(2 * time.Minute),
		WithLoadBalancer("sticky"),
		WithNodeID("node-7"),
	} {
		opt(&cfg)
	}

	if !cfg.EnableSessionResumption || cfg.SessionResumptionTTL != 2*time.Minute {
		t.Errorf("session resumption = %v/%v, want true/%v", cfg.EnableSessionResumption, cfg.SessionResumptionTTL, 2*time.Minute)
	}

	if !cfg.EnableLoadBalancer || cfg.LoadBalancerStrategy != "sticky" {
		t.Errorf("load balancer = %v/%q, want true/%q", cfg.EnableLoadBalancer, cfg.LoadBalancerStrategy, "sticky")
	}

	if cfg.NodeID != "node-7" {
		t.Errorf("NodeID = %q, want %q", cfg.NodeID, "node-7")
	}
}
