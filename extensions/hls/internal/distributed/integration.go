package distributed

import (
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus"
)

// RegisterStateMachine registers the HLS state machine with the consensus service
// This should be called during extension initialization
func RegisterStateMachine(consensusSvc consensus.ConsensusService, stateMachine *HLSStateMachine, logger forge.Logger) error {
	// Get the underlying state machine interface from consensus
	// Note: The actual integration depends on the consensus extension's API
	// This is a placeholder that shows the intended integration pattern

	logger.Info("registering HLS state machine with consensus service")

	// In a real implementation, this would call something like:
	// consensusSvc.RegisterStateMachine(stateMachine)
	// or
	// consensusSvc.SetStateMachine(stateMachine)

	// For now, log that registration would happen here
	logger.Warn("state machine registration requires consensus service API extension",
		forge.F("note", "state machine is ready but needs consensus integration API"),
	)

	return nil
}

// IntegrateWithConsensus ensures the coordinator's state machine is properly integrated
func (c *Coordinator) IntegrateWithConsensus() error {
	if c.stateMachine == nil {
		return fmt.Errorf("state machine not initialized")
	}

	// Register the state machine with consensus
	return RegisterStateMachine(c.consensus, c.stateMachine, c.logger)
}

// GetStateMachine returns the state machine (for testing or direct access)
func (c *Coordinator) GetStateMachine() *HLSStateMachine {
	return c.stateMachine
}
