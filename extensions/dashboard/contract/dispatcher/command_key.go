package dispatcher

import (
	"context"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

type commandKeyContextKey struct{}

// CommandIdempotencyKey returns the current command envelope's key. It is empty
// outside dispatch, in queries, or when a command supplied no key. Payload and
// route parameters cannot replace it. The key conveys no authorization.
//
// Use this with BypassIdempotency when your handler owns durable command receipts.
// Your service must validate the key and recheck current authority on retries.
func CommandIdempotencyKey(ctx context.Context) string {
	key, _ := ctx.Value(commandKeyContextKey{}).(string)

	return key
}

func withCommandIdempotencyKey(ctx context.Context, req contract.Request) context.Context {
	key := ""
	if req.Kind == contract.KindCommand {
		key = req.IdempotencyKey
	}

	// Every dispatch masks an outer command, including keyless commands and queries.
	return context.WithValue(ctx, commandKeyContextKey{}, key)
}
