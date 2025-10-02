package election

import (
	"context"
	"fmt"
	"sync"
	"time"

	json "github.com/json-iterator/go"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/consensus/storage"
	"github.com/xraph/forge/pkg/consensus/transport"
	"github.com/xraph/forge/pkg/logger"
)

// Manager manages the election process
type Manager struct {
	nodeID         string
	term           uint64
	state          ElectionState
	timeoutManager *TimeoutManager
	voteCollector  *VoteCollector
	transport      transport.Transport
	storage        storage.Storage
	validator      VoteValidator
	logger         common.Logger
	metrics        common.Metrics
	mu             sync.RWMutex

	// Election configuration
	config ElectionConfig

	// Election state
	currentLeader   string
	votedFor        string
	electionHistory []ElectionRecord
	callbacks       []ElectionCallback
	callbackMu      sync.RWMutex

	// Control channels
	stopCh       chan struct{}
	electionCh   chan ElectionEvent
	heartbeatCh  chan HeartbeatEvent
	started      bool
	startTime    time.Time
	lastElection time.Time
}

// ElectionState represents the current state of the election
type ElectionState string

const (
	ElectionStateFollower  ElectionState = "follower"
	ElectionStateCandidate ElectionState = "candidate"
	ElectionStateLeader    ElectionState = "leader"
	ElectionStateStopped   ElectionState = "stopped"
)

// ElectionConfig contains configuration for the election
type ElectionConfig struct {
	NodeID              string        `json:"node_id"`
	ClusterNodes        []string      `json:"cluster_nodes"`
	ElectionTimeout     time.Duration `json:"election_timeout"`
	HeartbeatInterval   time.Duration `json:"heartbeat_interval"`
	VoteTimeout         time.Duration `json:"vote_timeout"`
	MaxElectionAttempts int           `json:"max_election_attempts"`
	EnablePreVote       bool          `json:"enable_pre_vote"`
	PersistVotes        bool          `json:"persist_votes"`
	RequireSignatures   bool          `json:"require_signatures"`
}

// ElectionMessageHandler handles incoming election messages
type ElectionMessageHandler interface {
	HandleVoteRequest(ctx context.Context, request VoteRequest) (*VoteResponse, error)
	HandleVoteResponse(ctx context.Context, response VoteResponse) error
	HandleHeartbeat(ctx context.Context, heartbeat HeartbeatMessage) error
}

// ElectionCallback is called when election state changes
type ElectionCallback func(event ElectionEvent) error

// ElectionEvent represents an election event
type ElectionEvent struct {
	Type      ElectionEventType `json:"type"`
	NodeID    string            `json:"node_id"`
	Term      uint64            `json:"term"`
	State     ElectionState     `json:"state"`
	Leader    string            `json:"leader"`
	Timestamp time.Time         `json:"timestamp"`
	Data      interface{}       `json:"data"`
}

// ElectionEventType represents the type of election event
type ElectionEventType string

const (
	ElectionEventTypeStarted    ElectionEventType = "started"
	ElectionEventTypeTimeout    ElectionEventType = "timeout"
	ElectionEventTypeVoteGrant  ElectionEventType = "vote_grant"
	ElectionEventTypeVoteDeny   ElectionEventType = "vote_deny"
	ElectionEventTypeElected    ElectionEventType = "elected"
	ElectionEventTypeDefeated   ElectionEventType = "defeated"
	ElectionEventTypeHeartbeat  ElectionEventType = "heartbeat"
	ElectionEventTypeTermChange ElectionEventType = "term_change"
)

// ElectionRecord represents a historical election record
type ElectionRecord = storage.ElectionRecord

// HeartbeatMessage represents a heartbeat message
type HeartbeatMessage struct {
	LeaderID  string      `json:"leader_id"`
	Term      uint64      `json:"term"`
	Timestamp time.Time   `json:"timestamp"`
	Data      interface{} `json:"data"`
}

// HeartbeatEvent represents a heartbeat event
type HeartbeatEvent struct {
	LeaderID  string    `json:"leader_id"`
	Term      uint64    `json:"term"`
	Timestamp time.Time `json:"timestamp"`
	Received  time.Time `json:"received"`
}

// NewElectionManager creates a new election manager
func NewElectionManager(config ElectionConfig, transport transport.Transport, storage storage.Storage, validator VoteValidator, l common.Logger, metrics common.Metrics) *Manager {
	// Set defaults
	if config.ElectionTimeout == 0 {
		config.ElectionTimeout = 5 * time.Second
	}
	if config.HeartbeatInterval == 0 {
		config.HeartbeatInterval = 1 * time.Second
	}
	if config.VoteTimeout == 0 {
		config.VoteTimeout = 30 * time.Second
	}
	if config.MaxElectionAttempts == 0 {
		config.MaxElectionAttempts = 3
	}

	// Create timeout manager
	timeoutConfig := TimeoutConfig{
		MinTimeout: config.ElectionTimeout,
		MaxTimeout: config.ElectionTimeout * 2,
		Jitter:     0.1,
	}
	timeoutManager := NewTimeoutManager(timeoutConfig, l, metrics)

	// Create vote collector
	voteConfig := VoteCollectorConfig{
		NodeID:              config.NodeID,
		TotalNodes:          len(config.ClusterNodes),
		VoteTimeout:         config.VoteTimeout,
		RequireSignatures:   config.RequireSignatures,
		AllowDuplicateVotes: false,
		PersistVotes:        config.PersistVotes,
	}
	voteCollector := NewVoteCollector(voteConfig, validator, storage, l, metrics)

	em := &Manager{
		nodeID:          config.NodeID,
		term:            0,
		state:           ElectionStateFollower,
		timeoutManager:  timeoutManager,
		voteCollector:   voteCollector,
		transport:       transport,
		storage:         storage,
		validator:       validator,
		logger:          l,
		metrics:         metrics,
		config:          config,
		electionHistory: make([]ElectionRecord, 0),
		callbacks:       make([]ElectionCallback, 0),
		stopCh:          make(chan struct{}),
		electionCh:      make(chan ElectionEvent, 100),
		heartbeatCh:     make(chan HeartbeatEvent, 100),
		startTime:       time.Now(),
	}

	// Set up timeout callback
	timeoutManager.AddCallback(em.onElectionTimeout)

	// // Set up transport handler
	// transport.RegisterHandler(em)

	if l != nil {
		l.Info("election manager created",
			logger.String("node_id", config.NodeID),
			logger.Int("cluster_size", len(config.ClusterNodes)),
			logger.Duration("election_timeout", config.ElectionTimeout),
			logger.Duration("heartbeat_interval", config.HeartbeatInterval),
		)
	}

	return em
}

// Start starts the election manager
func (em *Manager) Start(ctx context.Context) error {
	em.mu.Lock()
	defer em.mu.Unlock()

	if em.started {
		return common.ErrLifecycleError("start", fmt.Errorf("election manager already started"))
	}

	// Load persisted state
	if err := em.loadPersistedState(ctx); err != nil {
		if em.logger != nil {
			em.logger.Warn("failed to load persisted state", logger.Error(err))
		}
	}

	// OnStart transport
	if err := em.transport.Start(ctx); err != nil {
		return common.ErrServiceStartFailed("transport", err)
	}

	// OnStart timeout manager
	em.timeoutManager.Start(ctx)

	// OnStart event processing
	go em.processEvents(ctx)

	// OnStart as follower
	em.state = ElectionStateFollower
	em.started = true

	// Send started event
	em.sendElectionEvent(ElectionEventTypeStarted, nil)

	if em.logger != nil {
		em.logger.Info("election manager started",
			logger.String("node_id", em.nodeID),
			logger.Uint64("term", em.term),
			logger.String("state", string(em.state)),
		)
	}

	if em.metrics != nil {
		em.metrics.Counter("forge.consensus.election.manager_started").Inc()
	}

	return nil
}

// Stop stops the election manager
func (em *Manager) Stop(ctx context.Context) error {
	em.mu.Lock()
	defer em.mu.Unlock()

	if !em.started {
		return common.ErrLifecycleError("stop", fmt.Errorf("election manager not started"))
	}

	// Signal stop
	close(em.stopCh)

	// OnStop timeout manager
	em.timeoutManager.Stop()

	// OnStop transport
	if err := em.transport.Stop(ctx); err != nil {
		if em.logger != nil {
			em.logger.Error("failed to stop transport", logger.Error(err))
		}
	}

	// Change state
	em.state = ElectionStateStopped
	em.started = false

	if em.logger != nil {
		em.logger.Info("election manager stopped")
	}

	if em.metrics != nil {
		em.metrics.Counter("forge.consensus.election.manager_stopped").Inc()
	}

	return nil
}

// GetState returns the current election state
func (em *Manager) GetState() ElectionState {
	em.mu.RLock()
	defer em.mu.RUnlock()

	return em.state
}

// GetTerm returns the current term
func (em *Manager) GetTerm() uint64 {
	em.mu.RLock()
	defer em.mu.RUnlock()

	return em.term
}

// GetLeader returns the current leader
func (em *Manager) GetLeader() string {
	em.mu.RLock()
	defer em.mu.RUnlock()

	return em.currentLeader
}

// IsLeader returns true if this node is the leader
func (em *Manager) IsLeader() bool {
	em.mu.RLock()
	defer em.mu.RUnlock()

	return em.state == ElectionStateLeader
}

// StartElection starts a new election
func (em *Manager) StartElection(ctx context.Context) error {
	em.mu.Lock()
	defer em.mu.Unlock()

	if !em.started {
		return common.ErrLifecycleError("start_election", fmt.Errorf("election manager not started"))
	}

	if em.state == ElectionStateLeader {
		return fmt.Errorf("already leader")
	}

	// Increment term
	em.term++
	em.state = ElectionStateCandidate
	em.votedFor = em.nodeID
	em.currentLeader = ""

	// Store updated term and vote
	if err := em.storage.StoreTerm(ctx, em.term); err != nil {
		if em.logger != nil {
			em.logger.Warn("failed to store term", logger.Error(err))
		}
	}

	if err := em.storage.StoreVote(ctx, em.term, em.nodeID); err != nil {
		if em.logger != nil {
			em.logger.Warn("failed to store vote", logger.Error(err))
		}
	}

	// Reset timeout
	em.timeoutManager.Reset()

	// Start vote collection
	if err := em.voteCollector.StartCollection(ctx, em.term); err != nil {
		return fmt.Errorf("failed to start vote collection: %w", err)
	}

	// Send vote requests to all nodes
	go em.sendVoteRequests(ctx)

	// Send election started event
	em.sendElectionEvent(ElectionEventTypeStarted, map[string]interface{}{
		"term":      em.term,
		"candidate": em.nodeID,
	})

	if em.logger != nil {
		em.logger.Info("election started",
			logger.String("node_id", em.nodeID),
			logger.Uint64("term", em.term),
		)
	}

	if em.metrics != nil {
		em.metrics.Counter("forge.consensus.election.elections_started").Inc()
	}

	return nil
}

// SendHeartbeat sends a heartbeat to all nodes
func (em *Manager) SendHeartbeat(ctx context.Context) error {
	em.mu.RLock()
	defer em.mu.RUnlock()

	if em.state != ElectionStateLeader {
		return fmt.Errorf("not leader")
	}

	heartbeat := HeartbeatMessage{
		LeaderID:  em.nodeID,
		Term:      em.term,
		Timestamp: time.Now(),
	}

	// Serialize the heartbeat
	heartbeatData, err := json.Marshal(heartbeat)
	if err != nil {
		return fmt.Errorf("failed to serialize heartbeat: %w", err)
	}

	// Send heartbeat to all nodes
	for _, nodeID := range em.config.ClusterNodes {
		if nodeID != em.nodeID {
			go func(targetNodeID string) {
				// Get target address
				address, err := em.transport.GetAddress(targetNodeID)
				if err != nil {
					if em.logger != nil {
						em.logger.Debug("failed to get address for node",
							logger.String("target_node", targetNodeID),
							logger.Error(err),
						)
					}
					return
				}

				// Create message
				message := transport.Message{
					Type:      transport.MessageTypeHeartbeat,
					From:      em.nodeID,
					To:        targetNodeID,
					Data:      heartbeatData,
					Timestamp: time.Now(),
					ID:        transport.GenerateMessageID(),
				}

				// Send heartbeat
				if err := em.transport.Send(ctx, address, message); err != nil {
					if em.logger != nil {
						em.logger.Debug("failed to send heartbeat",
							logger.String("target_node", targetNodeID),
							logger.Error(err),
						)
					}
				}
			}(nodeID)
		}
	}

	if em.metrics != nil {
		em.metrics.Counter("forge.consensus.election.heartbeats_sent").Inc()
	}

	return nil
}

// AddCallback adds an election callback
func (em *Manager) AddCallback(callback ElectionCallback) {
	em.callbackMu.Lock()
	defer em.callbackMu.Unlock()

	em.callbacks = append(em.callbacks, callback)
}

// RemoveCallback removes an election callback
func (em *Manager) RemoveCallback(callback ElectionCallback) {
	em.callbackMu.Lock()
	defer em.callbackMu.Unlock()

	for i, cb := range em.callbacks {
		if &cb == &callback {
			em.callbacks = append(em.callbacks[:i], em.callbacks[i+1:]...)
			break
		}
	}
}

// GetStats returns election statistics
func (em *Manager) GetStats() ElectionStats {
	em.mu.RLock()
	defer em.mu.RUnlock()

	return ElectionStats{
		NodeID:        em.nodeID,
		Term:          em.term,
		State:         em.state,
		Leader:        em.currentLeader,
		VotedFor:      em.votedFor,
		ElectionCount: len(em.electionHistory),
		LastElection:  em.lastElection,
		TimeoutStats:  em.timeoutManager.GetStats(),
		VoteStats:     em.voteCollector.GetStats(),
		Uptime:        time.Since(em.startTime),
		Started:       em.started,
	}
}

// ElectionStats contains election statistics
type ElectionStats struct {
	NodeID        string        `json:"node_id"`
	Term          uint64        `json:"term"`
	State         ElectionState `json:"state"`
	Leader        string        `json:"leader"`
	VotedFor      string        `json:"voted_for"`
	ElectionCount int           `json:"election_count"`
	LastElection  time.Time     `json:"last_election"`
	TimeoutStats  TimeoutStats  `json:"timeout_stats"`
	VoteStats     VoteStats     `json:"vote_stats"`
	Uptime        time.Duration `json:"uptime"`
	Started       bool          `json:"started"`
}

// GetElectionHistory returns the election history
func (em *Manager) GetElectionHistory() []ElectionRecord {
	em.mu.RLock()
	defer em.mu.RUnlock()

	history := make([]ElectionRecord, len(em.electionHistory))
	copy(history, em.electionHistory)
	return history
}

// ElectionMessageHandler implementation

// HandleVoteRequest handles incoming vote requests
func (em *Manager) HandleVoteRequest(ctx context.Context, request VoteRequest) (*VoteResponse, error) {
	em.mu.Lock()
	defer em.mu.Unlock()

	if em.logger != nil {
		em.logger.Debug("received vote request",
			logger.String("candidate", request.CandidateID),
			logger.Uint64("term", request.Term),
			logger.Uint64("last_log_index", request.LastLogIndex),
			logger.Uint64("last_log_term", request.LastLogTerm),
		)
	}

	// Validate request
	if em.validator != nil {
		if err := em.validator.ValidateVoteRequest(ctx, &request); err != nil {
			return &VoteResponse{
				Term:        em.term,
				VoteGranted: false,
				VoterID:     em.nodeID,
			}, err
		}
	}

	response := &VoteResponse{
		Term:        em.term,
		VoteGranted: false,
		VoterID:     em.nodeID,
	}

	// If request term is higher, update our term
	if request.Term > em.term {
		em.term = request.Term
		em.state = ElectionStateFollower
		em.votedFor = ""
		em.currentLeader = ""

		// Store updated state
		if err := em.storage.StoreTerm(ctx, em.term); err != nil {
			if em.logger != nil {
				em.logger.Warn("failed to store term", logger.Error(err))
			}
		}

		// Reset timeout
		em.timeoutManager.Reset()

		// Send term change event
		em.sendElectionEvent(ElectionEventTypeTermChange, map[string]interface{}{
			"old_term": em.term,
			"new_term": request.Term,
		})
	}

	// Grant vote if conditions are met
	if request.Term == em.term && (em.votedFor == "" || em.votedFor == request.CandidateID) {
		// Additional checks for log consistency would go here
		em.votedFor = request.CandidateID
		response.VoteGranted = true
		response.Term = em.term

		// Store vote
		if err := em.storage.StoreVote(ctx, em.term, request.CandidateID); err != nil {
			if em.logger != nil {
				em.logger.Warn("failed to store vote", logger.Error(err))
			}
		}

		// Reset timeout since we granted a vote
		em.timeoutManager.Reset()

		// Send vote grant event
		em.sendElectionEvent(ElectionEventTypeVoteGrant, map[string]interface{}{
			"candidate": request.CandidateID,
			"term":      request.Term,
		})

		if em.logger != nil {
			em.logger.Info("vote granted",
				logger.String("candidate", request.CandidateID),
				logger.Uint64("term", request.Term),
			)
		}
	} else {
		// Send vote deny event
		em.sendElectionEvent(ElectionEventTypeVoteDeny, map[string]interface{}{
			"candidate": request.CandidateID,
			"term":      request.Term,
			"reason":    "already_voted_or_stale_term",
		})

		if em.logger != nil {
			em.logger.Debug("vote denied",
				logger.String("candidate", request.CandidateID),
				logger.Uint64("term", request.Term),
				logger.String("voted_for", em.votedFor),
			)
		}
	}

	if em.metrics != nil {
		em.metrics.Counter("forge.consensus.election.vote_requests_handled").Inc()
		if response.VoteGranted {
			em.metrics.Counter("forge.consensus.election.votes_granted").Inc()
		} else {
			em.metrics.Counter("forge.consensus.election.votes_denied").Inc()
		}
	}

	return response, nil
}

// HandleVoteResponse handles incoming vote responses
func (em *Manager) HandleVoteResponse(ctx context.Context, response VoteResponse) error {
	em.mu.Lock()
	defer em.mu.Unlock()

	if em.state != ElectionStateCandidate {
		return nil // Ignore if not candidate
	}

	if response.Term > em.term {
		em.term = response.Term
		em.state = ElectionStateFollower
		em.votedFor = ""
		em.currentLeader = ""

		// Store updated state
		if err := em.storage.StoreTerm(ctx, em.term); err != nil {
			if em.logger != nil {
				em.logger.Warn("failed to store term", logger.Error(err))
			}
		}

		// Reset timeout
		em.timeoutManager.Reset()

		return nil
	}

	if response.Term < em.term {
		return nil // Ignore stale responses
	}

	// Record vote response
	if err := em.voteCollector.RecordVoteResponse(ctx, &response); err != nil {
		if em.logger != nil {
			em.logger.Warn("failed to record vote response", logger.Error(err))
		}
		return err
	}

	// Check if we have majority
	if em.voteCollector.HasMajority() {
		em.becomeLeader(ctx)
	} else if em.voteCollector.HasFailed() {
		em.becomeFollower(ctx)
	}

	return nil
}

// HandleHeartbeat handles incoming heartbeat messages
func (em *Manager) HandleHeartbeat(ctx context.Context, heartbeat HeartbeatMessage) error {
	em.mu.Lock()
	defer em.mu.Unlock()

	if em.logger != nil {
		em.logger.Debug("received heartbeat",
			logger.String("leader", heartbeat.LeaderID),
			logger.Uint64("term", heartbeat.Term),
		)
	}

	// Validate heartbeat
	if em.validator != nil {
		// if err := em.validator.ValidateHeartbeat(ctx, heartbeat); err != nil {
		// 	return err
		// }
	}

	// Update term if heartbeat term is higher
	if heartbeat.Term > em.term {
		em.term = heartbeat.Term
		em.state = ElectionStateFollower
		em.votedFor = ""

		// Store updated state
		if err := em.storage.StoreTerm(ctx, em.term); err != nil {
			if em.logger != nil {
				em.logger.Warn("failed to store term", logger.Error(err))
			}
		}
	}

	// Set leader if heartbeat is from current term
	if heartbeat.Term == em.term {
		em.currentLeader = heartbeat.LeaderID
		em.state = ElectionStateFollower

		// Reset timeout since we heard from leader
		em.timeoutManager.Reset()
	}

	// Send heartbeat event
	em.sendElectionEvent(ElectionEventTypeHeartbeat, map[string]interface{}{
		"leader": heartbeat.LeaderID,
		"term":   heartbeat.Term,
	})

	if em.metrics != nil {
		em.metrics.Counter("forge.consensus.election.heartbeats_received").Inc()
	}

	return nil
}

// Internal methods

// onElectionTimeout handles election timeout
func (em *Manager) onElectionTimeout(ctx context.Context, timeout time.Duration) {
	em.mu.RLock()
	state := em.state
	em.mu.RUnlock()

	if state == ElectionStateFollower || state == ElectionStateCandidate {
		// Send timeout event
		em.sendElectionEvent(ElectionEventTypeTimeout, map[string]interface{}{
			"timeout": timeout,
			"state":   state,
		})

		// OnStart new election
		if err := em.StartElection(ctx); err != nil {
			if em.logger != nil {
				em.logger.Error("failed to start election on timeout", logger.Error(err))
			}
		}
	}
}

// sendVoteRequests sends vote requests to all nodes
func (em *Manager) sendVoteRequests(ctx context.Context) {
	request := VoteRequest{
		Term:         em.term,
		CandidateID:  em.nodeID,
		LastLogIndex: 0, // TODO: Get from log
		LastLogTerm:  0, // TODO: Get from log
	}

	// Serialize the vote request
	requestData, err := json.Marshal(request)
	if err != nil {
		if em.logger != nil {
			em.logger.Error("failed to serialize vote request", logger.Error(err))
		}
		return
	}

	for _, nodeID := range em.config.ClusterNodes {
		if nodeID != em.nodeID {
			go func(targetNodeID string) {
				// Get target address
				address, err := em.transport.GetAddress(targetNodeID)
				if err != nil {
					if em.logger != nil {
						em.logger.Debug("failed to get address for node",
							logger.String("target_node", targetNodeID),
							logger.Error(err),
						)
					}
					return
				}

				// Create message
				message := transport.Message{
					Type:      transport.MessageTypeVoteRequest,
					From:      em.nodeID,
					To:        targetNodeID,
					Data:      requestData,
					Timestamp: time.Now(),
					ID:        transport.GenerateMessageID(),
				}

				// Send vote request
				if err := em.transport.Send(ctx, address, message); err != nil {
					if em.logger != nil {
						em.logger.Debug("failed to send vote request",
							logger.String("target_node", targetNodeID),
							logger.Error(err),
						)
					}
					return
				}
			}(nodeID)
		}
	}
}

// becomeLeader transitions to leader state
func (em *Manager) becomeLeader(ctx context.Context) {
	em.state = ElectionStateLeader
	em.currentLeader = em.nodeID
	em.lastElection = time.Now()

	// OnStop timeout (leaders don't need election timeouts)
	em.timeoutManager.Stop()

	// End vote collection
	em.voteCollector.EndCollection()

	// Record election
	em.recordElection(ctx, em.nodeID, "majority_votes")

	// Send elected event
	em.sendElectionEvent(ElectionEventTypeElected, map[string]interface{}{
		"term":   em.term,
		"leader": em.nodeID,
	})

	if em.logger != nil {
		em.logger.Info("became leader",
			logger.String("node_id", em.nodeID),
			logger.Uint64("term", em.term),
		)
	}

	if em.metrics != nil {
		em.metrics.Counter("forge.consensus.election.leader_elected").Inc()
	}

	// OnStart sending heartbeats
	go em.sendHeartbeats(ctx)
}

// becomeFollower transitions to follower state
func (em *Manager) becomeFollower(ctx context.Context) {
	em.state = ElectionStateFollower
	em.currentLeader = ""

	// Restart timeout
	em.timeoutManager.Reset()

	// End vote collection
	em.voteCollector.EndCollection()

	// Record election failure
	em.recordElection(ctx, "", "insufficient_votes")

	// Send defeated event
	em.sendElectionEvent(ElectionEventTypeDefeated, map[string]interface{}{
		"term":   em.term,
		"reason": "insufficient_votes",
	})

	if em.logger != nil {
		em.logger.Info("became follower",
			logger.String("node_id", em.nodeID),
			logger.Uint64("term", em.term),
		)
	}

	if em.metrics != nil {
		em.metrics.Counter("forge.consensus.election.leader_defeated").Inc()
	}
}

// sendHeartbeats sends periodic heartbeats as leader
func (em *Manager) sendHeartbeats(ctx context.Context) {
	ticker := time.NewTicker(em.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-em.stopCh:
			return
		case <-ticker.C:
			if em.GetState() != ElectionStateLeader {
				return
			}

			if err := em.SendHeartbeat(ctx); err != nil {
				if em.logger != nil {
					em.logger.Error("failed to send heartbeat", logger.Error(err))
				}
			}
		}
	}
}

// sendElectionEvent sends an election event
func (em *Manager) sendElectionEvent(eventType ElectionEventType, data interface{}) {
	event := ElectionEvent{
		Type:      eventType,
		NodeID:    em.nodeID,
		Term:      em.term,
		State:     em.state,
		Leader:    em.currentLeader,
		Timestamp: time.Now(),
		Data:      data,
	}

	select {
	case em.electionCh <- event:
	default:
		// Channel full, drop event
	}
}

// processEvents processes election events
func (em *Manager) processEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-em.stopCh:
			return
		case event := <-em.electionCh:
			em.processElectionEvent(event)
		case heartbeat := <-em.heartbeatCh:
			em.processHeartbeatEvent(heartbeat)
		}
	}
}

// processElectionEvent processes an election event
func (em *Manager) processElectionEvent(event ElectionEvent) {
	// Call callbacks
	em.callbackMu.RLock()
	callbacks := make([]ElectionCallback, len(em.callbacks))
	copy(callbacks, em.callbacks)
	em.callbackMu.RUnlock()

	for _, callback := range callbacks {
		go func(cb ElectionCallback) {
			defer func() {
				if r := recover(); r != nil {
					if em.logger != nil {
						em.logger.Error("election callback panic",
							logger.String("panic", fmt.Sprintf("%v", r)),
						)
					}
				}
			}()

			if err := cb(event); err != nil {
				if em.logger != nil {
					em.logger.Error("election callback error", logger.Error(err))
				}
			}
		}(callback)
	}
}

// processHeartbeatEvent processes a heartbeat event
func (em *Manager) processHeartbeatEvent(heartbeat HeartbeatEvent) {
	// Update metrics
	if em.metrics != nil {
		em.metrics.Counter("forge.consensus.election.heartbeats_processed").Inc()
	}
}

// recordElection records an election in history
func (em *Manager) recordElection(ctx context.Context, winner, reason string) {
	record := storage.ElectionRecord{
		Term:         em.term,
		Winner:       winner,
		StartTime:    time.Now().Add(-time.Since(em.lastElection)),
		EndTime:      time.Now(),
		Duration:     time.Since(em.lastElection),
		VoteCount:    em.voteCollector.GetStats().VotesReceived,
		Participants: em.config.ClusterNodes,
		Reason:       reason,
	}

	em.electionHistory = append(em.electionHistory, record)

	// Keep only last 100 records
	if len(em.electionHistory) > 100 {
		em.electionHistory = em.electionHistory[len(em.electionHistory)-100:]
	}

	// Persist if storage is available
	if em.storage != nil {
		if err := em.storage.StoreElectionRecord(ctx, record); err != nil {
			if em.logger != nil {
				em.logger.Warn("failed to persist election record", logger.Error(err))
			}
		}
	}
}

// loadPersistedState loads persisted election state
func (em *Manager) loadPersistedState(ctx context.Context) error {
	if em.storage == nil {
		return nil
	}

	// Load term
	if term, err := em.storage.GetTerm(ctx); err == nil {
		em.term = term
	}

	// Load voted for
	if votedFor, err := em.storage.GetVotedFor(ctx, em.term); err == nil {
		em.votedFor = votedFor
	}

	// Load election history
	if history, err := em.storage.GetElectionHistory(ctx, 100); err == nil {
		em.electionHistory = history
	}

	return nil
}
