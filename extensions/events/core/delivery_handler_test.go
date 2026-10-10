package core

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type reflectedDeliveryHandler struct{}

func (*reflectedDeliveryHandler) Foo()                                      {}
func (*reflectedDeliveryHandler) HandleTrade(context.Context, *Event) error { return nil }

func TestReflectionHandlerIgnoresShortMethodNames(t *testing.T) {
	require.NotPanics(t, func() { NewReflectionEventHandler("trade-handler", &reflectedDeliveryHandler{}) })
}
