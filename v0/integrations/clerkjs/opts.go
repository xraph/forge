package clerkjs

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/forge/pkg/common"
)

// UserCreatedHook handles new user creation events
type UserCreatedHook struct{}

func (h *UserCreatedHook) Name() string          { return "clerk-user-created" }
func (h *UserCreatedHook) Type() common.HookType { return "user_created" }
func (h *UserCreatedHook) Priority() int         { return 50 }

func (h *UserCreatedHook) Execute(ctx context.Context, data common.HookData) (common.HookResult, error) {
	// Default implementation - can be overridden by applications
	user, ok := data.Data.(*User)
	if !ok {
		return common.HookResult{Continue: true}, fmt.Errorf("invalid data type for user created hook")
	}

	// Log new user creation
	fmt.Printf("New user created: %s (%s)\n", user.Email, user.ClerkID)

	return common.HookResult{
		Continue: true,
		Data:     user,
		Metadata: map[string]interface{}{
			"processed_by": "clerk-user-created-hook",
			"timestamp":    time.Now(),
		},
	}, nil
}

// UserUpdatedHook handles user update events
type UserUpdatedHook struct{}

func (h *UserUpdatedHook) Name() string          { return "clerk-user-updated" }
func (h *UserUpdatedHook) Type() common.HookType { return "user_updated" }
func (h *UserUpdatedHook) Priority() int         { return 50 }

func (h *UserUpdatedHook) Execute(ctx context.Context, data common.HookData) (common.HookResult, error) {
	user, ok := data.Data.(*User)
	if !ok {
		return common.HookResult{Continue: true}, fmt.Errorf("invalid data type for user updated hook")
	}

	fmt.Printf("User updated: %s (%s)\n", user.Email, user.ClerkID)

	return common.HookResult{
		Continue: true,
		Data:     user,
		Metadata: map[string]interface{}{
			"processed_by": "clerk-user-updated-hook",
			"timestamp":    time.Now(),
		},
	}, nil
}

// UserDeletedHook handles user deletion events
type UserDeletedHook struct{}

func (h *UserDeletedHook) Name() string          { return "clerk-user-deleted" }
func (h *UserDeletedHook) Type() common.HookType { return "user_deleted" }
func (h *UserDeletedHook) Priority() int         { return 50 }

func (h *UserDeletedHook) Execute(ctx context.Context, data common.HookData) (common.HookResult, error) {
	dataMap, ok := data.Data.(map[string]interface{})
	if !ok {
		return common.HookResult{Continue: true}, fmt.Errorf("invalid data type for user deleted hook")
	}

	clerkID, _ := dataMap["clerk_id"].(string)
	fmt.Printf("User deleted: %s\n", clerkID)

	return common.HookResult{
		Continue: true,
		Data:     dataMap,
		Metadata: map[string]interface{}{
			"processed_by": "clerk-user-deleted-hook",
			"timestamp":    time.Now(),
		},
	}, nil
}

// SessionCreatedHook handles session creation events
type SessionCreatedHook struct{}

func (h *SessionCreatedHook) Name() string          { return "clerk-session-created" }
func (h *SessionCreatedHook) Type() common.HookType { return "session_created" }
func (h *SessionCreatedHook) Priority() int         { return 50 }

func (h *SessionCreatedHook) Execute(ctx context.Context, data common.HookData) (common.HookResult, error) {
	sessionData, ok := data.Data.(map[string]interface{})
	if !ok {
		return common.HookResult{Continue: true}, fmt.Errorf("invalid data type for session created hook")
	}

	sessionID, _ := sessionData["id"].(string)
	userID, _ := sessionData["user_id"].(string)
	fmt.Printf("Session created: %s for user %s\n", sessionID, userID)

	return common.HookResult{
		Continue: true,
		Data:     sessionData,
		Metadata: map[string]interface{}{
			"processed_by": "clerk-session-created-hook",
			"timestamp":    time.Now(),
		},
	}, nil
}

// SessionEndedHook handles session end events
type SessionEndedHook struct{}

func (h *SessionEndedHook) Name() string          { return "clerk-session-ended" }
func (h *SessionEndedHook) Type() common.HookType { return "session_ended" }
func (h *SessionEndedHook) Priority() int         { return 50 }

func (h *SessionEndedHook) Execute(ctx context.Context, data common.HookData) (common.HookResult, error) {
	sessionData, ok := data.Data.(map[string]interface{})
	if !ok {
		return common.HookResult{Continue: true}, fmt.Errorf("invalid data type for session ended hook")
	}

	sessionID, _ := sessionData["id"].(string)
	userID, _ := sessionData["user_id"].(string)
	fmt.Printf("Session ended: %s for user %s\n", sessionID, userID)

	return common.HookResult{
		Continue: true,
		Data:     sessionData,
		Metadata: map[string]interface{}{
			"processed_by": "clerk-session-ended-hook",
			"timestamp":    time.Now(),
		},
	}, nil
}
