package contract

import (
	"bytes"
	_ "embed"
	"fmt"

	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
	"github.com/xraph/forge/extensions/dashboard/contract/loader"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

//go:embed manifest.yaml
var manifestYAML []byte

// ManagerProvider returns the streaming Manager. Resolved at call time so the
// contributor's lifecycle can be independent of the dashboard's startup order
// (the manager may not be ready when contract handlers are registered).
type ManagerProvider func() streaming.Manager

// ConfigProvider returns the live streaming Config snapshot.
type ConfigProvider func() streaming.Config

// Deps bundles the data sources every handler needs.
type Deps struct {
	Manager ManagerProvider
	Config  ConfigProvider
}

// Register loads the embedded manifest, validates it, registers it with the
// dashboard contract registry, and wires the per-intent handlers against the
// dispatcher.
func Register(disp *dispatcher.Dispatcher, contractReg dashcontract.Registry, wreg dashcontract.WardenRegistry, deps Deps) error {
	if deps.Manager == nil {
		return fmt.Errorf("streaming/contract: Manager provider is required")
	}
	if deps.Config == nil {
		return fmt.Errorf("streaming/contract: Config provider is required")
	}

	m, err := loader.Load(bytes.NewReader(manifestYAML), "streaming/contract/manifest.yaml")
	if err != nil {
		return fmt.Errorf("streaming/contract: load manifest: %w", err)
	}
	if err := loader.Validate(m, wreg); err != nil {
		return fmt.Errorf("streaming/contract: validate manifest: %w", err)
	}
	if err := contractReg.Register(m); err != nil {
		return fmt.Errorf("streaming/contract: register manifest: %w", err)
	}

	const c = "streaming-contract"

	// Reads
	if err := dispatcher.RegisterQuery(disp, c, "stats", 1, statsHandler(deps.Manager)); err != nil {
		return err
	}
	if err := dispatcher.RegisterQuery(disp, c, "connections.list", 1, connectionsListHandler(deps.Manager)); err != nil {
		return err
	}
	if err := dispatcher.RegisterQuery(disp, c, "rooms.list", 1, roomsListHandler(deps.Manager)); err != nil {
		return err
	}
	if err := dispatcher.RegisterQuery(disp, c, "rooms.detail", 1, roomDetailHandler(deps.Manager)); err != nil {
		return err
	}
	if err := dispatcher.RegisterQuery(disp, c, "rooms.members", 1, roomMembersHandler(deps.Manager)); err != nil {
		return err
	}
	if err := dispatcher.RegisterQuery(disp, c, "rooms.moderation", 1, roomModerationHandler(deps.Manager)); err != nil {
		return err
	}
	if err := dispatcher.RegisterQuery(disp, c, "channels.list", 1, channelsListHandler(deps.Manager)); err != nil {
		return err
	}
	if err := dispatcher.RegisterQuery(disp, c, "presence.list", 1, presenceListHandler(deps.Manager)); err != nil {
		return err
	}
	if err := dispatcher.RegisterQuery(disp, c, "config", 1, configHandler(deps.Config)); err != nil {
		return err
	}

	// Mutations
	if err := dispatcher.RegisterCommand(disp, c, "rooms.create", 1, createRoomHandler(deps.Manager)); err != nil {
		return err
	}
	if err := dispatcher.RegisterCommand(disp, c, "rooms.delete", 1, deleteRoomHandler(deps.Manager)); err != nil {
		return err
	}
	if err := dispatcher.RegisterCommand(disp, c, "rooms.send-message", 1, sendMessageHandler(deps.Manager)); err != nil {
		return err
	}
	if err := dispatcher.RegisterCommand(disp, c, "presence.set", 1, setPresenceHandler(deps.Manager)); err != nil {
		return err
	}
	if err := dispatcher.RegisterCommand(disp, c, "connections.kick", 1, kickConnectionHandler(deps.Manager)); err != nil {
		return err
	}

	return nil
}
