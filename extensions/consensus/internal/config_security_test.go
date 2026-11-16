package internal

import (
	"errors"
	"testing"
)

func validConfig() Config {
	c := DefaultConfig()
	c.NodeID = "n1"

	return c
}

func TestDefaultConfigBindsLoopback(t *testing.T) {
	if got := DefaultConfig().BindAddr; got != "127.0.0.1" {
		t.Fatalf("default bind address %q, want 127.0.0.1", got)
	}

	c := validConfig()
	if err := c.Validate(); err != nil {
		t.Fatalf("default config does not validate: %v", err)
	}
}

func TestValidateRejectsOptionsThatCannotBeHonoured(t *testing.T) {
	cases := map[string]func(*Config){
		"quic transport":         func(c *Config) { c.Transport.Type = "quic" },
		"encryption at rest":     func(c *Config) { c.Security.EnableEncryption = true },
		"admin auth without key": func(c *Config) { c.AdminAPI.EnableAuth = true },
	}

	for name, mutate := range cases {
		c := validConfig()
		mutate(&c)

		if err := c.Validate(); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("%s: Validate returned %v, want ErrInvalidConfig", name, err)
		}
	}

	c := validConfig()
	c.Transport.Type = "http"
	c.AdminAPI.EnableAuth = true
	c.AdminAPI.APIKey = "k"

	if err := c.Validate(); err != nil {
		t.Fatalf("http transport with admin auth rejected: %v", err)
	}
}
