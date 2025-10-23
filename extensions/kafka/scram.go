package kafka

import (
	"crypto/sha256"
	"crypto/sha512"
	"hash"

	"github.com/xdg-go/scram"
)

// HashGeneratorFcn is a function type for generating hash functions
type HashGeneratorFcn func() hash.Hash

var (
	// SHA256 is a hash generator for SHA-256
	SHA256 HashGeneratorFcn = sha256.New

	// SHA512 is a hash generator for SHA-512
	SHA512 HashGeneratorFcn = sha512.New
)

// XDGSCRAMClient implements the SCRAM client interface
type XDGSCRAMClient struct {
	*scram.Client
	*scram.ClientConversation
	scram.HashGeneratorFcn
}

// Begin starts the SCRAM authentication process
func (x *XDGSCRAMClient) Begin(userName, password, authzID string) (err error) {
	x.Client, err = x.HashGeneratorFcn.NewClient(userName, password, authzID)
	if err != nil {
		return err
	}
	x.ClientConversation = x.Client.NewConversation()
	return nil
}

// Step processes one step of the SCRAM authentication
func (x *XDGSCRAMClient) Step(challenge string) (response string, err error) {
	response, err = x.ClientConversation.Step(challenge)
	return
}

// Done returns true if the authentication is complete
func (x *XDGSCRAMClient) Done() bool {
	return x.ClientConversation.Done()
}
