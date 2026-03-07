package streaming

import (
	"context"
	"sync"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// StreamingHook is the base interface for all streaming hooks.
// Hooks implement one or more of the optional hook interfaces below.
type StreamingHook interface {
	Name() string
}

// ConnectionHook fires on connection lifecycle events.
type ConnectionHook interface {
	StreamingHook
	// OnConnect is called before registration. Return error to reject connection.
	OnConnect(ctx context.Context, conn Connection) error
	// OnDisconnect is called after unregistration.
	OnDisconnect(ctx context.Context, conn Connection)
}

// MessageHook fires on message events.
type MessageHook interface {
	StreamingHook
	// OnMessageReceived is called before message processing. Can transform or block (return nil).
	OnMessageReceived(ctx context.Context, conn Connection, msg *Message) (*Message, error)
	// OnMessageDelivered is called after delivery (non-blocking, runs async).
	OnMessageDelivered(ctx context.Context, conn Connection, msg *Message)
}

// RawMessageHook fires before deserialization on raw bytes from the connection.
type RawMessageHook interface {
	StreamingHook
	// OnRawMessage processes raw bytes before decoding. Return error to drop the message.
	OnRawMessage(ctx context.Context, conn Connection, data []byte) ([]byte, error)
}

// RoomHook fires on room lifecycle events.
type RoomHook interface {
	StreamingHook
	// OnRoomJoin is called before join. Return error to reject.
	OnRoomJoin(ctx context.Context, conn Connection, roomID string) error
	// OnRoomLeave is called after leave.
	OnRoomLeave(ctx context.Context, conn Connection, roomID string)
	// OnRoomCreate is called before room creation. Return error to reject.
	OnRoomCreate(ctx context.Context, room Room) error
	// OnRoomDelete is called after room deletion.
	OnRoomDelete(ctx context.Context, roomID string)
}

// PresenceHook fires on presence changes.
type PresenceHook interface {
	StreamingHook
	// OnPresenceChange is called after a user's presence status changes.
	OnPresenceChange(ctx context.Context, userID, oldStatus, newStatus string)
}

// ErrorHook fires on message handling errors.
type ErrorHook interface {
	StreamingHook
	// OnError is called when a message handling error occurs.
	OnError(ctx context.Context, conn Connection, err error)
}

// HookRegistry manages streaming hooks and dispatches events.
type HookRegistry struct {
	mu    sync.RWMutex
	hooks map[string]StreamingHook

	// Pre-categorized for fast dispatch (rebuilt on add/remove).
	connectionHooks []ConnectionHook
	messageHooks    []MessageHook
	rawMessageHooks []RawMessageHook
	roomHooks       []RoomHook
	presenceHooks   []PresenceHook
	errorHooks      []ErrorHook
}

// NewHookRegistry creates a new hook registry.
func NewHookRegistry() *HookRegistry {
	return &HookRegistry{
		hooks: make(map[string]StreamingHook),
	}
}

// Add registers a hook. The hook is type-asserted to categorize it
// into the appropriate dispatch lists.
func (r *HookRegistry) Add(hook StreamingHook) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.hooks[hook.Name()] = hook
	r.rebuild()
}

// Remove unregisters a hook by name.
func (r *HookRegistry) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.hooks, name)
	r.rebuild()
}

// List returns all registered hooks.
func (r *HookRegistry) List() []StreamingHook {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]StreamingHook, 0, len(r.hooks))
	for _, h := range r.hooks {
		result = append(result, h)
	}

	return result
}

// rebuild categorizes hooks by interface. Must be called with lock held.
func (r *HookRegistry) rebuild() {
	r.connectionHooks = r.connectionHooks[:0]
	r.messageHooks = r.messageHooks[:0]
	r.rawMessageHooks = r.rawMessageHooks[:0]
	r.roomHooks = r.roomHooks[:0]
	r.presenceHooks = r.presenceHooks[:0]
	r.errorHooks = r.errorHooks[:0]

	for _, h := range r.hooks {
		if ch, ok := h.(ConnectionHook); ok {
			r.connectionHooks = append(r.connectionHooks, ch)
		}

		if mh, ok := h.(MessageHook); ok {
			r.messageHooks = append(r.messageHooks, mh)
		}

		if rmh, ok := h.(RawMessageHook); ok {
			r.rawMessageHooks = append(r.rawMessageHooks, rmh)
		}

		if rh, ok := h.(RoomHook); ok {
			r.roomHooks = append(r.roomHooks, rh)
		}

		if ph, ok := h.(PresenceHook); ok {
			r.presenceHooks = append(r.presenceHooks, ph)
		}

		if eh, ok := h.(ErrorHook); ok {
			r.errorHooks = append(r.errorHooks, eh)
		}
	}
}

// snapshot helpers — copy slice under read lock to avoid holding lock during dispatch.

func (r *HookRegistry) connectionHooksCopy() []ConnectionHook {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cp := make([]ConnectionHook, len(r.connectionHooks))
	copy(cp, r.connectionHooks)

	return cp
}

func (r *HookRegistry) messageHooksCopy() []MessageHook {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cp := make([]MessageHook, len(r.messageHooks))
	copy(cp, r.messageHooks)

	return cp
}

func (r *HookRegistry) rawMessageHooksCopy() []RawMessageHook {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cp := make([]RawMessageHook, len(r.rawMessageHooks))
	copy(cp, r.rawMessageHooks)

	return cp
}

func (r *HookRegistry) roomHooksCopy() []RoomHook {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cp := make([]RoomHook, len(r.roomHooks))
	copy(cp, r.roomHooks)

	return cp
}

func (r *HookRegistry) presenceHooksCopy() []PresenceHook {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cp := make([]PresenceHook, len(r.presenceHooks))
	copy(cp, r.presenceHooks)

	return cp
}

func (r *HookRegistry) errorHooksCopy() []ErrorHook {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cp := make([]ErrorHook, len(r.errorHooks))
	copy(cp, r.errorHooks)

	return cp
}

// --- Fire methods ---

// FireOnConnect fires ConnectionHook.OnConnect for all registered hooks.
// Returns the first error encountered, which should be used to reject the connection.
func (r *HookRegistry) FireOnConnect(ctx context.Context, conn streaming.EnhancedConnection) error {
	for _, h := range r.connectionHooksCopy() {
		if err := h.OnConnect(ctx, conn); err != nil {
			return err
		}
	}

	return nil
}

// FireOnDisconnect fires ConnectionHook.OnDisconnect for all registered hooks.
// Errors are ignored (post-hook).
func (r *HookRegistry) FireOnDisconnect(ctx context.Context, conn streaming.EnhancedConnection) {
	for _, h := range r.connectionHooksCopy() {
		h.OnDisconnect(ctx, conn)
	}
}

// FireOnMessageReceived fires MessageHook.OnMessageReceived for all hooks in sequence.
// Each hook can transform or block (return nil) the message.
func (r *HookRegistry) FireOnMessageReceived(ctx context.Context, conn streaming.EnhancedConnection, msg *streaming.Message) (*streaming.Message, error) {
	current := msg

	for _, h := range r.messageHooksCopy() {
		result, err := h.OnMessageReceived(ctx, conn, current)
		if err != nil {
			return nil, err
		}

		if result == nil {
			return nil, nil // message blocked
		}

		current = result
	}

	return current, nil
}

// FireOnMessageDelivered fires MessageHook.OnMessageDelivered asynchronously.
// This is non-blocking to avoid slowing down message delivery.
func (r *HookRegistry) FireOnMessageDelivered(ctx context.Context, conn streaming.EnhancedConnection, msg *streaming.Message) {
	hooks := r.messageHooksCopy()
	if len(hooks) == 0 {
		return
	}

	go func() {
		for _, h := range hooks {
			h.OnMessageDelivered(ctx, conn, msg)
		}
	}()
}

// FireOnRawMessage fires RawMessageHook.OnRawMessage for all hooks in sequence.
// Each hook can transform the bytes or block (return error) the message.
func (r *HookRegistry) FireOnRawMessage(ctx context.Context, conn streaming.EnhancedConnection, data []byte) ([]byte, error) {
	current := data

	for _, h := range r.rawMessageHooksCopy() {
		result, err := h.OnRawMessage(ctx, conn, current)
		if err != nil {
			return nil, err
		}

		current = result
	}

	return current, nil
}

// FireOnRoomJoin fires RoomHook.OnRoomJoin for all hooks.
// Returns the first error encountered, which should be used to reject the join.
func (r *HookRegistry) FireOnRoomJoin(ctx context.Context, conn streaming.EnhancedConnection, roomID string) error {
	for _, h := range r.roomHooksCopy() {
		if err := h.OnRoomJoin(ctx, conn, roomID); err != nil {
			return err
		}
	}

	return nil
}

// FireOnRoomLeave fires RoomHook.OnRoomLeave for all hooks (post-hook).
func (r *HookRegistry) FireOnRoomLeave(ctx context.Context, conn streaming.EnhancedConnection, roomID string) {
	for _, h := range r.roomHooksCopy() {
		h.OnRoomLeave(ctx, conn, roomID)
	}
}

// FireOnRoomCreate fires RoomHook.OnRoomCreate for all hooks.
// Returns the first error encountered, which should be used to reject creation.
func (r *HookRegistry) FireOnRoomCreate(ctx context.Context, room streaming.Room) error {
	for _, h := range r.roomHooksCopy() {
		if err := h.OnRoomCreate(ctx, room); err != nil {
			return err
		}
	}

	return nil
}

// FireOnRoomDelete fires RoomHook.OnRoomDelete for all hooks (post-hook).
func (r *HookRegistry) FireOnRoomDelete(ctx context.Context, roomID string) {
	for _, h := range r.roomHooksCopy() {
		h.OnRoomDelete(ctx, roomID)
	}
}

// FireOnPresenceChange fires PresenceHook.OnPresenceChange for all hooks (post-hook).
func (r *HookRegistry) FireOnPresenceChange(ctx context.Context, userID, oldStatus, newStatus string) {
	for _, h := range r.presenceHooksCopy() {
		h.OnPresenceChange(ctx, userID, oldStatus, newStatus)
	}
}

// FireOnError fires ErrorHook.OnError for all hooks (post-hook).
func (r *HookRegistry) FireOnError(ctx context.Context, conn streaming.EnhancedConnection, err error) {
	for _, h := range r.errorHooksCopy() {
		h.OnError(ctx, conn, err)
	}
}
