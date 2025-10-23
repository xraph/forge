package kafka

import (
	"crypto/sha256"
	"crypto/sha512"
	"testing"
)

func TestHashGenerators(t *testing.T) {
	// Test SHA256
	sha256Hash := SHA256()
	if sha256Hash == nil {
		t.Error("expected SHA256 hash generator to return non-nil")
	}

	// Test SHA512
	sha512Hash := SHA512()
	if sha512Hash == nil {
		t.Error("expected SHA512 hash generator to return non-nil")
	}

	// Verify they produce different hash types
	sha256Result := sha256Hash.Sum(nil)
	sha512Result := sha512Hash.Sum(nil)

	if len(sha256Result) != sha256.Size {
		t.Errorf("expected SHA256 hash size %d, got %d", sha256.Size, len(sha256Result))
	}

	if len(sha512Result) != sha512.Size {
		t.Errorf("expected SHA512 hash size %d, got %d", sha512.Size, len(sha512Result))
	}
}

func TestXDGSCRAMClient(t *testing.T) {
	client := &XDGSCRAMClient{
		HashGeneratorFcn: SHA256,
	}

	// Test Begin
	err := client.Begin("testuser", "testpass", "")
	if err != nil {
		t.Fatalf("Begin failed: %v", err)
	}

	if client.Client == nil {
		t.Error("expected Client to be initialized")
	}

	if client.ClientConversation == nil {
		t.Error("expected ClientConversation to be initialized")
	}

	// Test Done - should be false initially
	if client.Done() {
		t.Error("expected Done to be false initially")
	}
}

func TestXDGSCRAMClientSHA512(t *testing.T) {
	client := &XDGSCRAMClient{
		HashGeneratorFcn: SHA512,
	}

	err := client.Begin("testuser", "testpass", "")
	if err != nil {
		t.Fatalf("Begin with SHA512 failed: %v", err)
	}

	if client.Client == nil {
		t.Error("expected Client to be initialized with SHA512")
	}
}

func TestXDGSCRAMClientStep(t *testing.T) {
	client := &XDGSCRAMClient{
		HashGeneratorFcn: SHA256,
	}

	err := client.Begin("testuser", "testpass", "")
	if err != nil {
		t.Fatalf("Begin failed: %v", err)
	}

	// Get first step
	response, err := client.Step("")
	if err != nil {
		t.Fatalf("Step failed: %v", err)
	}

	if response == "" {
		t.Error("expected non-empty response from first step")
	}
}

func TestXDGSCRAMClientEmptyPassword(t *testing.T) {
	client := &XDGSCRAMClient{
		HashGeneratorFcn: SHA256,
	}

	err := client.Begin("testuser", "", "")
	if err != nil {
		t.Fatalf("Begin with empty password failed: %v", err)
	}
}

func TestXDGSCRAMClientWithAuthzID(t *testing.T) {
	client := &XDGSCRAMClient{
		HashGeneratorFcn: SHA256,
	}

	err := client.Begin("testuser", "testpass", "authzuser")
	if err != nil {
		t.Fatalf("Begin with authzID failed: %v", err)
	}
}
