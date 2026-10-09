// Package contract exposes the Conduit runtime to Forge dashboard clients.
package contract

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"strings"
	"time"

	"github.com/xraph/forge/extensions/conduit/core"
	dash "github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
	"github.com/xraph/forge/extensions/dashboard/contract/loader"
)

//go:embed manifest.yaml
var manifest []byte

// Deps resolves the runtime when an intent executes.
type Deps struct{ Runtime func() *core.Runtime }

// Register installs typed, scoped intents and the dashboard manifest.
func Register(disp *dispatcher.Dispatcher, registry dash.Registry, wardens dash.WardenRegistry, deps Deps) error {
	if deps.Runtime == nil {
		return errors.New("conduit/contract: runtime provider is required")
	}

	m, err := loader.Load(bytes.NewReader(manifest), "conduit/contract/manifest.yaml")
	if err != nil {
		return err
	}

	if err := loader.Validate(m, wardens); err != nil {
		return err
	}

	if err := registry.Register(m); err != nil {
		return err
	}

	if err := dispatcher.RegisterQuery(disp, "conduit", "overview", 1, overview(deps)); err != nil {
		return err
	}

	if err := dispatcher.RegisterQuery(disp, "conduit", "services.list", 1, services(deps)); err != nil {
		return err
	}

	if err := dispatcher.RegisterQuery(disp, "conduit", "deadletters.list", 1, deadletters(deps)); err != nil {
		return err
	}

	if err := dispatcher.RegisterQuery(disp, "conduit", "hooks.list", 1, hooks(deps)); err != nil {
		return err
	}

	if err := dispatcher.RegisterQuery(disp, "conduit", "consumers.list", 1, consumers(deps)); err != nil {
		return err
	}

	if err := dispatcher.RegisterQuery(disp, "conduit", "backfills.list", 1, backfills(deps)); err != nil {
		return err
	}

	if err := dispatcher.RegisterCommand(disp, "conduit", "consumers.pause", 1, pause(deps, true)); err != nil {
		return err
	}

	if err := dispatcher.RegisterCommand(disp, "conduit", "consumers.resume", 1, pause(deps, false)); err != nil {
		return err
	}

	if err := dispatcher.RegisterCommand(disp, "conduit", "backfills.run", 1, backfill(deps)); err != nil {
		return err
	}

	return dispatcher.RegisterCommand(disp, "conduit", "deadletters.replay", 1, replay(deps))
}

// authorize rejects every present malformed or mismatched scope claim.
func authorize(deps Deps, principal dash.Principal) (*core.Runtime, error) {
	r := deps.Runtime()
	if r == nil {
		return nil, dash.ErrUnavailable
	}

	identity := r.Identity()
	for _, scope := range []struct{ key, value string }{{"namespace", identity.Namespace}, {"service_id", identity.ServiceID}} {
		if claim, present := principal.Claims[scope.key]; present {
			value, ok := claim.(string)
			if !ok || strings.TrimSpace(value) == "" || value != scope.value {
				return nil, dash.ErrPermissionDenied
			}
		}
	}

	return r, nil
}

func publicError(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, core.ErrNotFound) {
		return dash.ErrNotFound
	}

	if errors.Is(err, core.ErrConflict) {
		return dash.ErrConflict
	}

	return &dash.Error{Code: dash.CodeUnavailable, Message: "Communications provider is unavailable or does not support this operation", Retryable: true}
}

func overview(deps Deps) func(context.Context, struct{}, dash.Principal) (core.Snapshot, error) {
	return func(ctx context.Context, _ struct{}, p dash.Principal) (core.Snapshot, error) {
		r, err := authorize(deps, p)
		if err != nil {
			return core.Snapshot{}, err
		}

		result, err := r.Snapshot(ctx)

		return result, publicError(err)
	}
}

// ServiceList includes distinct replica records with one shared logical service ID.
type ServiceList struct {
	Instances []core.Instance `json:"instances"`
}

func services(deps Deps) func(context.Context, struct{}, dash.Principal) (ServiceList, error) {
	return func(ctx context.Context, _ struct{}, p dash.Principal) (ServiceList, error) {
		r, err := authorize(deps, p)
		if err != nil {
			return ServiceList{}, err
		}

		result, err := r.Instances(ctx)

		return ServiceList{Instances: result}, publicError(err)
	}
}

// ListInput selects a provider and an opaque continuation cursor.
type ListInput struct {
	Provider     string `json:"provider"`
	Subscription string `json:"subscription,omitempty"`
	Cursor       string `json:"cursor,omitempty"`
	Limit        int    `json:"limit,omitempty"`
}

// LetterSummary keeps payloads, headers and arbitrary handler error text out of operator listings.
type LetterSummary struct {
	ID          string            `json:"id"`
	MessageID   string            `json:"messageID"`
	MessageType string            `json:"messageType"`
	Delivery    core.DeliveryInfo `json:"delivery"`
	FailedAt    time.Time         `json:"failedAt"`
	Replayed    bool              `json:"replayed"`
}

// LetterList is cursor-paged and scoped to the runtime's logical service.
type LetterList struct {
	Letters    []LetterSummary `json:"letters"`
	NextCursor string          `json:"nextCursor"`
}

func deadletters(deps Deps) func(context.Context, ListInput, dash.Principal) (LetterList, error) {
	return func(ctx context.Context, in ListInput, p dash.Principal) (LetterList, error) {
		r, err := authorize(deps, p)
		if err != nil {
			return LetterList{}, err
		}

		if in.Provider == "" || in.Limit < 0 || in.Limit > 100 {
			return LetterList{}, dash.ErrBadRequest
		}

		if in.Limit == 0 {
			in.Limit = 25
		}

		letters, cursor, err := r.DeadLetters(ctx, in.Provider, in.Subscription, in.Cursor, in.Limit)
		if err != nil {
			return LetterList{}, publicError(err)
		}

		result := LetterList{Letters: make([]LetterSummary, 0, len(letters)), NextCursor: cursor}
		for _, letter := range letters {
			result.Letters = append(result.Letters, LetterSummary{ID: letter.ID, MessageID: letter.Message.ID, MessageType: letter.Message.Type, Delivery: letter.Delivery, FailedAt: letter.FailedAt, Replayed: letter.Replayed})
		}

		return result, nil
	}
}

// HookList is bounded and contains metadata only.
type HookList struct {
	Events []core.HookEvent `json:"events"`
}

func hooks(deps Deps) func(context.Context, struct{}, dash.Principal) (HookList, error) {
	return func(_ context.Context, _ struct{}, p dash.Principal) (HookList, error) {
		r, err := authorize(deps, p)
		if err != nil {
			return HookList{}, err
		}

		return HookList{Events: r.RecentEvents()}, nil
	}
}

// ReplayInput identifies one dead letter in a provider and logical subscription.
type ReplayInput struct {
	Provider     string `json:"provider"`
	Subscription string `json:"subscription"`
	ID           string `json:"id"`
}

func replay(deps Deps) func(context.Context, ReplayInput, dash.Principal) (core.Receipt, error) {
	return func(ctx context.Context, in ReplayInput, p dash.Principal) (core.Receipt, error) {
		r, err := authorize(deps, p)
		if err != nil {
			return core.Receipt{}, err
		}

		if in.Provider == "" || in.Subscription == "" || in.ID == "" {
			return core.Receipt{}, dash.ErrBadRequest
		}

		receipt, err := r.ReplayDeadLetter(ctx, in.Provider, in.Subscription, in.ID)

		return receipt, publicError(err)
	}
}

// ConsumerList reports real broker cursor state with instance-local latency.
type ConsumerList struct {
	Consumers []core.ConsumerInfo `json:"consumers"`
}

func consumers(deps Deps) func(context.Context, struct{}, dash.Principal) (ConsumerList, error) {
	return func(ctx context.Context, _ struct{}, principal dash.Principal) (ConsumerList, error) {
		r, err := authorize(deps, principal)
		if err != nil {
			return ConsumerList{}, err
		}

		rows, err := r.Consumers(ctx)

		return ConsumerList{Consumers: rows}, publicError(err)
	}
}

type PauseInput struct {
	Subscription string `json:"subscription"`
}

func pause(deps Deps, paused bool) func(context.Context, PauseInput, dash.Principal) (struct{}, error) {
	return func(ctx context.Context, in PauseInput, principal dash.Principal) (struct{}, error) {
		r, err := authorize(deps, principal)
		if err != nil {
			return struct{}{}, err
		}

		if in.Subscription == "" {
			return struct{}{}, dash.ErrBadRequest
		}

		return struct{}{}, publicError(r.PauseSubscription(ctx, in.Subscription, paused))
	}
}

// BackfillList retains service scope and opaque paging.
type BackfillList struct {
	Jobs       []core.Backfill `json:"jobs"`
	NextCursor string          `json:"nextCursor"`
}

func backfills(deps Deps) func(context.Context, ListInput, dash.Principal) (BackfillList, error) {
	return func(ctx context.Context, in ListInput, principal dash.Principal) (BackfillList, error) {
		r, err := authorize(deps, principal)
		if err != nil {
			return BackfillList{}, err
		}

		if in.Provider == "" || in.Limit < 0 || in.Limit > 100 {
			return BackfillList{}, dash.ErrBadRequest
		}

		if in.Limit == 0 {
			in.Limit = 25
		}

		rows, cursor, err := r.Backfills(ctx, in.Provider, in.Cursor, in.Limit)

		return BackfillList{Jobs: rows, NextCursor: cursor}, publicError(err)
	}
}
func backfill(deps Deps) func(context.Context, core.BackfillInput, dash.Principal) (core.Backfill, error) {
	return func(ctx context.Context, in core.BackfillInput, principal dash.Principal) (core.Backfill, error) {
		r, err := authorize(deps, principal)
		if err != nil {
			return core.Backfill{}, err
		}

		if in.Validate() != nil {
			return core.Backfill{}, dash.ErrBadRequest
		}

		job, err := r.Backfill(ctx, in)

		return job, publicError(err)
	}
}
