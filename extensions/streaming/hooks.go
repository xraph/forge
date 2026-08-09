package streaming

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

const (
	// deliveryQueueSize bounds how many pending delivery-hook batches the pool
	// holds. Beyond this, batches are dropped rather than queued: a broadcast to
	// a large room must not be throttled by hook bookkeeping.
	deliveryQueueSize = 1024

	// maxDeliveryWorkers caps the pool on high-core machines. Delivery hooks are
	// observability work, not the delivery path itself.
	maxDeliveryWorkers = 16
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
//
// Hooks fire in registration order. Several of the fire methods are a pipeline
// — FireOnMessageReceived threads the message through each hook in turn, and
// FireOnConnect stops at the first rejection — so the order has to be defined
// and stable, not whatever a map range happens to produce.
type HookRegistry struct {
	mu    sync.RWMutex
	hooks map[string]StreamingHook
	// order holds hook names in registration order and is the source of truth
	// for dispatch order; hooks is the lookup by name.
	order []string

	// Pre-categorized for fast dispatch (rebuilt on add/remove).
	connectionHooks []ConnectionHook
	messageHooks    []MessageHook
	rawMessageHooks []RawMessageHook
	roomHooks       []RoomHook
	presenceHooks   []PresenceHook
	errorHooks      []ErrorHook

	// Delivery hooks run on a small fixed worker pool instead of one goroutine
	// per delivery. The pool is started on first use, so a registry with no
	// message hooks costs nothing.
	deliveryOnce  sync.Once
	deliveryQueue chan deliveryJob
	deliveryWG    sync.WaitGroup

	// droppedDeliveries counts hook batches that were never run, so a silent
	// drop is at least a visible number.
	droppedDeliveries atomic.Uint64

	closeOnce sync.Once
	closed    chan struct{}
}

// deliveryJob is one batch of delivery hooks to run for a single recipient.
type deliveryJob struct {
	ctx   context.Context
	conn  streaming.EnhancedConnection
	msg   *streaming.Message
	hooks []MessageHook
}

func (j deliveryJob) run() {
	for _, h := range j.hooks {
		h.OnMessageDelivered(j.ctx, j.conn, j.msg)
	}
}

// NewHookRegistry creates a new hook registry.
func NewHookRegistry() *HookRegistry {
	return &HookRegistry{
		hooks:  make(map[string]StreamingHook),
		closed: make(chan struct{}),
	}
}

// Close shuts down the delivery worker pool and waits for in-flight delivery
// hooks to finish. It is safe to call concurrently and more than once; calls
// after the first are no-ops. Deliveries fired after Close are dropped.
func (r *HookRegistry) Close() error {
	r.closeOnce.Do(func() {
		close(r.closed)
	})

	r.deliveryWG.Wait()

	return nil
}

// ensureDeliveryPool starts the worker pool on first delivery.
func (r *HookRegistry) ensureDeliveryPool() {
	r.deliveryOnce.Do(func() {
		r.deliveryQueue = make(chan deliveryJob, deliveryQueueSize)

		workers := runtime.NumCPU()
		if workers < 1 {
			workers = 1
		}

		if workers > maxDeliveryWorkers {
			workers = maxDeliveryWorkers
		}

		r.deliveryWG.Add(workers)

		for i := 0; i < workers; i++ {
			go r.deliveryWorker()
		}
	})
}

func (r *HookRegistry) deliveryWorker() {
	defer r.deliveryWG.Done()

	for {
		select {
		case job := <-r.deliveryQueue:
			job.run()
		case <-r.closed:
			// Drain what was accepted before Close so a clean shutdown does not
			// silently swallow queued deliveries.
			for {
				select {
				case job := <-r.deliveryQueue:
					job.run()
				default:
					return
				}
			}
		}
	}
}

// Add registers a hook. The hook is type-asserted to categorize it
// into the appropriate dispatch lists.
//
// Registering a name that already exists replaces the hook in place, keeping
// its original position in the dispatch order.
func (r *HookRegistry) Add(hook StreamingHook) {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := hook.Name()
	if _, exists := r.hooks[name]; !exists {
		r.order = append(r.order, name)
	}

	r.hooks[name] = hook
	r.rebuild()
}

// Remove unregisters a hook by name.
func (r *HookRegistry) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.hooks[name]; !exists {
		return
	}

	delete(r.hooks, name)

	for i, n := range r.order {
		if n == name {
			r.order = append(r.order[:i], r.order[i+1:]...)

			break
		}
	}

	r.rebuild()
}

// List returns all registered hooks in registration order.
func (r *HookRegistry) List() []StreamingHook {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]StreamingHook, 0, len(r.order))
	for _, name := range r.order {
		result = append(result, r.hooks[name])
	}

	return result
}

// rebuild categorizes hooks by interface. Must be called with lock held.
//
// It walks r.order rather than ranging over the hooks map. Map iteration order
// is randomized, which made dispatch order vary run to run — so a hook chain
// whose members transform a message, or one that gates a connection before a
// second hook sees it, had no defined semantics. Registration order is the
// contract.
func (r *HookRegistry) rebuild() {
	r.connectionHooks = r.connectionHooks[:0]
	r.messageHooks = r.messageHooks[:0]
	r.rawMessageHooks = r.rawMessageHooks[:0]
	r.roomHooks = r.roomHooks[:0]
	r.presenceHooks = r.presenceHooks[:0]
	r.errorHooks = r.errorHooks[:0]

	for _, name := range r.order {
		h := r.hooks[name]

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

	// No message hooks is the common case on the delivery path; skip the
	// allocation entirely. Ranging over a nil slice is a no-op.
	if len(r.messageHooks) == 0 {
		return nil
	}

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

// FireOnMessageDelivered fires MessageHook.OnMessageDelivered on a bounded
// worker pool. This is non-blocking to avoid slowing down message delivery:
// the call is made once per recipient, so a broadcast to a large room reaches
// this path thousands of times for a single message.
//
// If the pool's queue is saturated the batch is dropped rather than queued or
// awaited, because blocking here would stall delivery itself.
func (r *HookRegistry) FireOnMessageDelivered(ctx context.Context, conn streaming.EnhancedConnection, msg *streaming.Message) {
	// The common case is no message hooks at all — do no work, not even a copy.
	hooks := r.messageHooksCopy()
	if len(hooks) == 0 {
		return
	}

	select {
	case <-r.closed:
		r.droppedDeliveries.Add(1)

		return
	default:
	}

	r.ensureDeliveryPool()

	job := deliveryJob{
		// Delivery hooks outlive the request that triggered them. The caller's
		// context is often already cancelled by the time a worker picks the job
		// up, so keep its values but drop its cancellation.
		ctx:   context.WithoutCancel(ctx),
		conn:  conn,
		msg:   msg,
		hooks: hooks,
	}

	select {
	case r.deliveryQueue <- job:
	default:
		// Queue full: drop. deliveryQueue is never closed, so this send stays
		// safe even when it races Close.
		r.droppedDeliveries.Add(1)
	}
}

// DroppedDeliveries returns the number of delivery-hook batches that were never
// run, either because the pool's queue was saturated or because the registry was
// already closed. Delivery hooks are non-blocking by contract, so overload is
// shed here rather than pushed back onto the delivery path — this counter is how
// that shedding becomes visible. A steadily climbing value means hooks are too
// slow for the message rate, and any hook doing billing or audit work is losing
// events.
func (r *HookRegistry) DroppedDeliveries() uint64 {
	return r.droppedDeliveries.Load()
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
