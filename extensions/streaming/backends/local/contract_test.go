package local

import (
	"context"
	"testing"

	"github.com/xraph/forge/extensions/streaming/backends/storetest"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// The local store is held to the same contract as Redis. Running one suite
// against both is what proves they agree — a cursor issued by a node using one
// backend is redeemed by a node that may be using the other.
func newContractStore(t *testing.T) streaming.MessageStore {
	t.Helper()

	store := NewMessageStore()

	if err := store.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	t.Cleanup(func() { _ = store.Disconnect(context.Background()) })

	return store
}

func TestLocalMessageStore_Contract(t *testing.T) {
	storetest.RunMessageStoreContract(t, newContractStore)
}
