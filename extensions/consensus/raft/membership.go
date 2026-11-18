package raft

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/consensus/internal"
)

var (
	// ErrConfigChangeInProgress is returned when a membership change is
	// requested while another one has not committed yet. Changes are made one
	// server at a time (Raft dissertation section 4.1).
	ErrConfigChangeInProgress = errors.New("raft: a membership change is already in progress")

	// ErrLeadershipTransferInProgress is returned for client requests and
	// membership changes while the leader is handing over leadership.
	ErrLeadershipTransferInProgress = errors.New("raft: leadership transfer in progress")

	// ErrLeadershipTransferFailed is returned when the target did not take
	// over within an election timeout. The old leader keeps leading.
	ErrLeadershipTransferFailed = errors.New("raft: leadership transfer did not complete in time")

	// ErrLearnerNotCaughtUp is returned by AddNode when the new server did not
	// catch up with the log in time. It is removed again.
	ErrLearnerNotCaughtUp = errors.New("raft: new server did not catch up with the log")
)

// configEntry is a cluster configuration and the log index that introduced
// it. Index 0 is the bootstrap configuration from AddPeer calls.
type configEntry struct {
	index     uint64
	cfg       internal.ClusterConfiguration
	bootstrap bool
}

// Configuration returns the current cluster configuration. It takes effect
// as soon as its entry is appended, before it commits.
func (n *Node) Configuration() internal.ClusterConfiguration {
	n.mu.RLock()
	defer n.mu.RUnlock()

	return n.currentConfigLocked().Clone()
}

func (n *Node) currentConfigLocked() internal.ClusterConfiguration {
	return n.configs[len(n.configs)-1].cfg
}

// configAtLocked returns the configuration in force at log index.
func (n *Node) configAtLocked(index uint64) internal.ClusterConfiguration {
	for _, c := range slices.Backward(n.configs) {
		if c.index <= index {
			return c.cfg
		}
	}

	return n.configs[0].cfg
}

// bootstrapConfig is the configuration before any configuration entry: this
// node as the only voter, or nothing at all for a node that joins an existing
// cluster.
func (n *Node) bootstrapConfig() configEntry {
	cfg := internal.ClusterConfiguration{}
	if !n.config.Join {
		cfg.Members = []internal.ClusterMember{{
			ID: n.id, Address: n.config.Address, Port: n.config.Port, Voter: true,
		}}
	}

	return configEntry{cfg: cfg, bootstrap: true}
}

// rebuildConfigsLocked resets the configuration history to base followed by
// every configuration entry still in the log, then applies the result.
func (n *Node) rebuildConfigsLocked(base configEntry) {
	n.configs = []configEntry{base}

	for _, e := range n.log.ConfigEntries() {
		if e.Index <= base.index {
			continue
		}

		if cfg, err := decodeConfig(e.Data); err == nil {
			n.configs = append(n.configs, configEntry{index: e.Index, cfg: cfg})
		} else {
			n.logger.Error("ignoring unreadable configuration entry",
				forge.F("node_id", n.id),
				forge.F("index", e.Index),
				forge.F("error", err),
			)
		}
	}

	n.applyConfigLocked()
}

// restoreConfigs builds the configuration history at startup from the
// snapshot and the log.
func (n *Node) restoreConfigs() {
	n.mu.Lock()
	defer n.mu.Unlock()

	base := n.bootstrapConfig()

	if cfg := n.log.SnapshotConfig(); cfg != nil {
		base = configEntry{index: n.log.SnapshotIndex(), cfg: cfg.Clone()}
	}

	n.rebuildConfigsLocked(base)
}

// addBootstrapPeer adds a voter to the bootstrap configuration. Once the
// log or a snapshot holds a configuration, membership only changes through
// AddNode and RemoveNode, and the call is ignored.
func (n *Node) addBootstrapPeer(peerID, address string, port int) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.config.Join || !n.configs[0].bootstrap {
		return
	}

	base := &n.configs[0].cfg
	for i, m := range base.Members {
		if m.ID == peerID {
			if address != "" {
				base.Members[i].Address, base.Members[i].Port = address, port
			}

			return
		}
	}

	base.Members = append(base.Members, internal.ClusterMember{ID: peerID, Address: address, Port: port, Voter: true})

	if len(n.configs) == 1 {
		n.applyConfigLocked()
	}
}

// AddPeerAddress adds a voter, with its network address, to the bootstrap
// configuration, like AddPeer. The address is recorded in configuration
// entries so servers that join later can reach it.
func (n *Node) AddPeerAddress(peerID, address string, port int) {
	n.addBootstrapPeer(peerID, address, port)
}

// appendConfigLocked records a configuration entry that was just appended.
func (n *Node) appendConfigLocked(index uint64, cfg internal.ClusterConfiguration) {
	n.configs = append(n.configs, configEntry{index: index, cfg: cfg})
	n.applyConfigLocked()

	n.logger.Info("cluster configuration changed",
		forge.F("node_id", n.id),
		forge.F("index", index),
		forge.F("voters", cfg.Voters()),
		forge.F("members", len(cfg.Members)),
	)
}

// truncateConfigsLocked drops configurations whose entries were truncated
// from the log, returning to the one before them.
func (n *Node) truncateConfigsLocked(after uint64) {
	keep := len(n.configs)
	for keep > 1 && n.configs[keep-1].index > after {
		keep--
	}

	if keep != len(n.configs) {
		n.configs = n.configs[:keep]
		n.applyConfigLocked()
	}
}

// compactConfigsLocked forgets configurations older than the one in force
// at the snapshot index.
func (n *Node) compactConfigsLocked(snapshotIndex uint64) {
	first := 0

	for i := range n.configs {
		if n.configs[i].index <= snapshotIndex {
			first = i
		}
	}

	n.configs = n.configs[first:]
}

// applyConfigLocked makes the replication state match the current
// configuration: every other member, voter or learner, gets a peer entry and
// a transport route; servers no longer in the configuration lose their peer
// entry, so they are neither replicated to nor counted.
func (n *Node) applyConfigLocked() {
	cfg := n.currentConfigLocked()
	lastIndex := n.log.LastIndex()

	n.peersLock.Lock()

	for _, m := range cfg.Members {
		if m.ID == n.id {
			continue
		}

		if p, ok := n.peers[m.ID]; ok {
			if m.Address != "" {
				p.Address, p.Port = m.Address, m.Port
			}
		} else {
			n.peers[m.ID] = &PeerState{
				ID: m.ID, Address: m.Address, Port: m.Port,
				NextIndex: lastIndex + 1, LastContact: time.Now(), ReplicationMu: sync.Mutex{},
			}
			n.nextIndex[m.ID] = lastIndex + 1
			n.matchIndex[m.ID] = 0
		}

		if m.Address != "" {
			if err := n.transport.AddPeer(m.ID, m.Address, m.Port); err != nil && !errors.Is(err, internal.ErrPeerExists) {
				n.logger.Warn("failed to add transport route for cluster member",
					forge.F("node_id", n.id),
					forge.F("peer", m.ID),
					forge.F("error", err),
				)
			}
		}
	}

	for id := range n.peers {
		if _, ok := cfg.Member(id); ok {
			continue
		}

		// The transport route stays: a removed leader still needs answers
		// to the AppendEntries that commit its own removal.
		delete(n.peers, id)
		delete(n.nextIndex, id)
		delete(n.matchIndex, id)
	}

	n.peersLock.Unlock()
}

// applyAppendedConfigsLocked records configuration entries among entries a
// follower just appended. Configurations take effect on append.
func (n *Node) applyAppendedConfigsLocked(entries []internal.LogEntry) {
	for _, e := range entries {
		if e.Type != internal.EntryConfig {
			continue
		}

		cfg, err := decodeConfig(e.Data)
		if err != nil {
			n.logger.Error("ignoring unreadable configuration entry",
				forge.F("node_id", n.id),
				forge.F("index", e.Index),
				forge.F("error", err),
			)

			continue
		}

		n.appendConfigLocked(e.Index, cfg)
	}
}

// votersLocked returns the voting members other than this node, and whether
// this node votes.
func (n *Node) votersLocked() (others []string, selfVotes bool) {
	for _, id := range n.currentConfigLocked().Voters() {
		if id == n.id {
			selfVotes = true
		} else {
			others = append(others, id)
		}
	}

	return others, selfVotes
}

func decodeConfig(data []byte) (internal.ClusterConfiguration, error) {
	var cfg internal.ClusterConfiguration
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("decode cluster configuration: %w", err)
	}

	return cfg, nil
}

// AddNode adds a server to the cluster. It first joins as a learner that
// receives the log without voting; once it has caught up it is promoted to a
// voter. Adding it as a voter straight away would let a slow, empty server
// count towards majorities and stall commits while it catches up (section
// 4.2.1). Each step is its own configuration entry, committed before the next
// is proposed. If the server does not catch up in time it is removed again
// and ErrLearnerNotCaughtUp is returned.
func (n *Node) AddNode(ctx context.Context, nodeID, address string, port int) error {
	if nodeID == "" {
		return errors.New("raft: node ID is required")
	}

	if err := n.beginChange(); err != nil {
		return err
	}
	defer n.endChange()

	n.mu.RLock()
	current := n.currentConfigLocked().Clone()
	n.mu.RUnlock()

	if m, ok := current.Member(nodeID); ok && m.Voter {
		return nil
	} else if !ok {
		learner := current.Clone()
		learner.Members = append(learner.Members, internal.ClusterMember{ID: nodeID, Address: address, Port: port})

		if err := n.proposeConfig(ctx, learner); err != nil {
			return fmt.Errorf("add %s as learner: %w", nodeID, err)
		}
	}

	if err := n.waitCaughtUp(ctx, nodeID); err != nil {
		rollback := n.Configuration()
		rollback.Members = withoutMember(rollback.Members, nodeID)

		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), n.config.ElectionTimeoutMax*4)
		defer cancel()

		if rbErr := n.proposeConfig(rollbackCtx, rollback); rbErr != nil {
			return errors.Join(err, fmt.Errorf("remove learner %s again: %w", nodeID, rbErr))
		}

		return err
	}

	promoted := n.Configuration()
	for i := range promoted.Members {
		if promoted.Members[i].ID == nodeID {
			promoted.Members[i].Voter = true
		}
	}

	if err := n.proposeConfig(ctx, promoted); err != nil {
		return fmt.Errorf("promote %s to voter: %w", nodeID, err)
	}

	return nil
}

// RemoveNode removes a server from the cluster. Removing the leader is
// allowed: it keeps leading until the new configuration commits (without
// counting itself), then steps down so the remaining servers elect a leader.
func (n *Node) RemoveNode(ctx context.Context, nodeID string) error {
	if err := n.beginChange(); err != nil {
		return err
	}
	defer n.endChange()

	cfg := n.Configuration()
	if _, ok := cfg.Member(nodeID); !ok {
		return nil
	}

	cfg.Members = withoutMember(cfg.Members, nodeID)
	if len(cfg.Voters()) == 0 {
		return errors.New("raft: cannot remove the last voting member")
	}

	return n.proposeConfig(ctx, cfg)
}

func withoutMember(members []internal.ClusterMember, id string) []internal.ClusterMember {
	out := make([]internal.ClusterMember, 0, len(members))

	for _, m := range members {
		if m.ID != id {
			out = append(out, m)
		}
	}

	return out
}

// beginChange claims the single membership change slot.
func (n *Node) beginChange() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.GetRole() != internal.RoleLeader {
		return internal.NewNotLeaderError(n.id, n.GetLeader())
	}

	if n.transferTarget != "" {
		return ErrLeadershipTransferInProgress
	}

	if n.changing || n.configs[len(n.configs)-1].index > n.commitIndex {
		return ErrConfigChangeInProgress
	}

	n.changing = true

	return nil
}

func (n *Node) endChange() {
	n.mu.Lock()
	n.changing = false
	n.mu.Unlock()
}

// proposeConfig appends a configuration entry, which takes effect at once,
// and waits until it is committed and applied.
func (n *Node) proposeConfig(ctx context.Context, cfg internal.ClusterConfiguration) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode cluster configuration: %w", err)
	}

	entry := internal.LogEntry{Type: internal.EntryConfig, Data: data}

	return n.appendAndWait(ctx, entry, true, func(index uint64) {
		n.appendConfigLocked(index, cfg)
	})
}

// waitCaughtUp waits until a learner holds every committed entry, bounded
// by ctx and twenty election timeouts.
func (n *Node) waitCaughtUp(ctx context.Context, nodeID string) error {
	deadline := time.Now().Add(20 * n.config.ElectionTimeoutMax)
	tick := max(n.config.HeartbeatInterval/2, 5*time.Millisecond)

	for {
		n.mu.RLock()
		leader := n.GetRole() == internal.RoleLeader
		match := n.matchIndex[nodeID]
		target := n.commitIndex
		n.mu.RUnlock()

		if !leader {
			return internal.NewNotLeaderError(n.id, n.GetLeader())
		}

		if match >= target {
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("%w: %s holds index %d, commit index is %d", ErrLearnerNotCaughtUp, nodeID, match, target)
		}

		n.triggerReplication()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-n.doneCh():
			return internal.ErrNotStarted
		case <-time.After(tick):
		}
	}
}

// TransferLeadership hands leadership to target (dissertation section 3.10).
// The leader stops accepting client requests and membership changes, brings
// the target's log up to date, and sends it TimeoutNow so it starts an
// election at once. If no new leader has taken over within an election
// timeout the transfer is abandoned, the leader resumes normal operation and
// ErrLeadershipTransferFailed is returned.
func (n *Node) TransferLeadership(ctx context.Context, targetNodeID string) error {
	n.mu.Lock()

	if n.GetRole() != internal.RoleLeader {
		n.mu.Unlock()

		return internal.NewNotLeaderError(n.id, n.GetLeader())
	}

	if targetNodeID == n.id {
		n.mu.Unlock()

		return nil
	}

	if !n.currentConfigLocked().IsVoter(targetNodeID) {
		n.mu.Unlock()

		return fmt.Errorf("raft: %s is not a voting member", targetNodeID)
	}

	if n.transferTarget != "" {
		n.mu.Unlock()

		return ErrLeadershipTransferInProgress
	}

	if n.changing {
		n.mu.Unlock()

		return ErrConfigChangeInProgress
	}

	term := n.currentTerm
	n.transferTarget = targetNodeID
	n.mu.Unlock()

	n.logger.Info("transferring leadership",
		forge.F("node_id", n.id),
		forge.F("target", targetNodeID),
		forge.F("term", term),
	)

	err := n.runTransfer(ctx, targetNodeID, term)

	n.mu.Lock()
	n.transferTarget = ""
	n.mu.Unlock()

	if err != nil {
		n.logger.Warn("leadership transfer abandoned",
			forge.F("node_id", n.id),
			forge.F("target", targetNodeID),
			forge.F("error", err),
		)
	}

	return err
}

func (n *Node) runTransfer(ctx context.Context, target string, term uint64) error {
	ctx, cancel := context.WithTimeout(ctx, n.config.ElectionTimeoutMax)
	defer cancel()

	tick := max(n.config.HeartbeatInterval/4, 2*time.Millisecond)

	stillLeading := func() bool {
		n.mu.RLock()
		defer n.mu.RUnlock()

		return n.GetRole() == internal.RoleLeader && n.currentTerm == term
	}

	// Bring the target up to date. No new entries arrive meanwhile.
	for {
		n.mu.RLock()
		caughtUp := n.matchIndex[target] >= n.log.LastIndex()
		n.mu.RUnlock()

		if caughtUp {
			break
		}

		if !stillLeading() {
			return nil
		}

		n.triggerReplication()

		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: %s did not catch up", ErrLeadershipTransferFailed, target)
		case <-time.After(tick):
		}
	}

	if _, err := n.call(ctx, target, internal.MessageTypeTimeoutNow,
		&internal.TimeoutNowRequest{Term: term, LeaderID: n.id}); err != nil && stillLeading() {
		return fmt.Errorf("%w: TimeoutNow to %s: %v", ErrLeadershipTransferFailed, target, err) //nolint:errorlint // the first error is the sentinel
	}

	// Wait for the target's election to depose this leader.
	for stillLeading() {
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: %s did not take over", ErrLeadershipTransferFailed, target)
		case <-time.After(tick):
		}
	}

	return nil
}

// handleTimeoutNow starts an election at once when the current leader asks.
func (n *Node) handleTimeoutNow(from string, req internal.TimeoutNowRequest) internal.TimeoutNowResponse {
	n.mu.RLock()
	resp := internal.TimeoutNowResponse{Term: n.currentTerm, NodeID: n.id}
	ok := req.Term == n.currentTerm && from == req.LeaderID && n.currentConfigLocked().IsVoter(n.id)
	n.mu.RUnlock()

	if !ok {
		return resp
	}

	n.logger.Info("starting election on leader's request",
		forge.F("node_id", n.id),
		forge.F("leader", req.LeaderID),
	)

	resp.Success = true

	go n.startElectionWith(true)

	return resp
}

// transferBlocksLocked reports whether client writes must wait for a
// leadership transfer.
func (n *Node) transferBlocksLocked() bool {
	return n.transferTarget != ""
}
