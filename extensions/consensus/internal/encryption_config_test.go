package internal

import (
	"errors"
	"testing"
)

func TestValidateEncryptionSettings(t *testing.T) {
	valid := map[string]func(*SecurityConfig){
		"key file": func(s *SecurityConfig) { s.EncryptionKeyFile = "/etc/consensus/raft.key" },
		"key env":  func(s *SecurityConfig) { s.EncryptionKeyEnv = "RAFT_KEY" },
		"inline only when allowed": func(s *SecurityConfig) {
			s.EncryptionKey = "00"
			s.AllowInlineEncryptionKey = true
		},
		"rotation list": func(s *SecurityConfig) {
			s.EncryptionKeys = []EncryptionKeyConfig{{ID: 1, File: "/k1"}, {ID: 2, Env: "K2"}}
			s.EncryptionActiveKeyID = 1
		},
	}

	for name, mutate := range valid {
		c := validConfig()
		c.Security.EnableEncryption = true
		mutate(&c.Security)

		if err := c.Validate(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}

	invalid := map[string]func(*SecurityConfig){
		"no key":           func(*SecurityConfig) {},
		"inline by itself": func(s *SecurityConfig) { s.EncryptionKey = "00" },
		"two single sources": func(s *SecurityConfig) {
			s.EncryptionKeyFile = "/k"
			s.EncryptionKeyEnv = "K"
		},
		"single and list": func(s *SecurityConfig) {
			s.EncryptionKeyFile = "/k"
			s.EncryptionKeys = []EncryptionKeyConfig{{ID: 2, File: "/k2"}}
		},
		"id zero": func(s *SecurityConfig) { s.EncryptionKeys = []EncryptionKeyConfig{{File: "/k"}} },
		"duplicate ids": func(s *SecurityConfig) {
			s.EncryptionKeys = []EncryptionKeyConfig{{ID: 1, File: "/a"}, {ID: 1, File: "/b"}}
		},
		"no source":   func(s *SecurityConfig) { s.EncryptionKeys = []EncryptionKeyConfig{{ID: 1}} },
		"two sources": func(s *SecurityConfig) { s.EncryptionKeys = []EncryptionKeyConfig{{ID: 1, File: "/a", Env: "A"}} },
		"inline in list": func(s *SecurityConfig) {
			s.EncryptionKeys = []EncryptionKeyConfig{{ID: 1, Value: "00"}}
		},
		"unknown active": func(s *SecurityConfig) {
			s.EncryptionKeyFile = "/k"
			s.EncryptionActiveKeyID = 2
		},
	}

	for name, mutate := range invalid {
		c := validConfig()
		c.Security.EnableEncryption = true
		mutate(&c.Security)

		if err := c.Validate(); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("%s: Validate returned %v, want ErrInvalidConfig", name, err)
		}
	}

	// Key settings are ignored while encryption is off.
	c := validConfig()
	c.Security.EncryptionKey = "00"

	if err := c.Validate(); err != nil {
		t.Fatalf("inline key with encryption off: %v", err)
	}

	if got := (SecurityConfig{EncryptionKeyEnv: "K"}).EncryptionKeySources(); len(got) != 1 || got[0].ID != 1 || got[0].Env != "K" {
		t.Fatalf("single key settings map to %+v, want key ID 1", got)
	}
}
