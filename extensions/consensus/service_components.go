package consensus

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/consensus/internal"
	"github.com/xraph/forge/extensions/consensus/raft"
	"github.com/xraph/forge/extensions/consensus/statemachine"
	"github.com/xraph/forge/extensions/consensus/storage"
	"github.com/xraph/forge/extensions/consensus/transport"
)

// startComponents builds and starts storage, transport, state machine and
// raft node from the configuration. On error the caller stops whatever was
// started.
func (s *Service) startComponents(ctx context.Context) error {
	if err := checkSupported(s.config); err != nil {
		return err
	}

	store, err := newStorage(s.config, s.logger)
	if err != nil {
		return fmt.Errorf("failed to create storage: %w", err)
	}

	store, err = protectStorage(s.config, store, s.logger)
	if err != nil {
		return fmt.Errorf("failed to set up storage encryption: %w", err)
	}

	if err := store.Start(ctx); err != nil {
		return fmt.Errorf("failed to start %s storage: %w", s.config.Storage.Type, err)
	}

	s.storage = store

	tr, err := newTransport(s.config, s.logger)
	if err != nil {
		return fmt.Errorf("failed to create transport: %w", err)
	}

	if err := tr.Start(ctx); err != nil {
		return fmt.Errorf("failed to start %s transport: %w", s.config.Transport.Type, err)
	}

	s.transport = tr

	sm := statemachine.NewMemoryStateMachine(statemachine.MemoryStateMachineConfig{}, s.logger)
	s.stateMachine = sm

	node, err := raft.NewNode(raftConfig(s.config), s.logger, sm, tr, store)
	if err != nil {
		return fmt.Errorf("failed to create raft node: %w", err)
	}

	for _, peer := range s.config.Peers {
		if peer.ID == s.config.NodeID {
			continue
		}

		// The node may already route to this peer: membership restored from
		// the log or a snapshot on restart adds routes for every member.
		if err := tr.AddPeer(peer.ID, peer.Address, peer.Port); err != nil && !errors.Is(err, internal.ErrPeerExists) {
			return fmt.Errorf("failed to add peer %s: %w", peer.ID, err)
		}

		// Configured peers only seed the membership of a brand new cluster.
		// Once the log or a snapshot holds a configuration this is ignored,
		// and a joining node only needs the routes added above.
		node.AddPeerAddress(peer.ID, peer.Address, peer.Port)
	}

	if err := node.Start(s.ctx); err != nil {
		return fmt.Errorf("failed to start raft node: %w", err)
	}

	s.node = node
	s.raftNode = node

	return nil
}

// stopComponents stops the raft node, transport and storage in that order.
func (s *Service) stopComponents(ctx context.Context) {
	stopCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if s.node != nil {
		if err := s.node.Stop(stopCtx); err != nil {
			s.logger.Error("failed to stop raft node", forge.F("error", err))
		}
	}

	if s.transport != nil {
		if err := s.transport.Stop(stopCtx); err != nil {
			s.logger.Error("failed to stop transport", forge.F("error", err))
		}
	}

	if s.storage != nil {
		if err := s.storage.Stop(stopCtx); err != nil {
			s.logger.Error("failed to stop storage", forge.F("error", err))
		}
	}
}

// checkSupported fails for settings the service cannot honour.
func checkSupported(cfg internal.Config) error {
	if cfg.Discovery.Type != "" && cfg.Discovery.Type != "static" {
		return fmt.Errorf("%w: discovery type %q is not wired into the consensus service; "+
			"list the cluster in peers and use discovery type static", internal.ErrInvalidConfig, cfg.Discovery.Type)
	}

	switch cfg.Storage.Type {
	case "badger", "boltdb", "memory":
	default:
		return fmt.Errorf("%w: storage type %q is not implemented; use badger, boltdb or memory",
			internal.ErrInvalidConfig, cfg.Storage.Type)
	}

	return nil
}

func newStorage(cfg internal.Config, logger forge.Logger) (internal.Storage, error) {
	switch cfg.Storage.Type {
	case "memory":
		return storage.NewMemoryStorage(storage.MemoryStorageConfig{}, logger), nil
	case "badger":
		if err := os.MkdirAll(cfg.Storage.Path, 0o700); err != nil {
			return nil, err
		}

		return storage.NewBadgerStorage(storage.BadgerStorageConfig{Path: cfg.Storage.Path, SyncWrites: cfg.Storage.SyncWrites}, logger)
	case "boltdb":
		if err := os.MkdirAll(cfg.Storage.Path, 0o700); err != nil {
			return nil, err
		}

		return storage.NewBoltDBStorage(storage.BoltDBStorageConfig{
			Path:       filepath.Join(cfg.Storage.Path, "raft.db"),
			Timeout:    cfg.Storage.BoltOptions.Timeout,
			NoSync:     cfg.Storage.BoltOptions.NoSync,
			NoGrowSync: cfg.Storage.BoltOptions.NoGrowSync,
		}, logger)
	default:
		return nil, fmt.Errorf("unsupported storage type %q", cfg.Storage.Type)
	}
}

// newTransport builds the configured transport. With security.enable_tls it
// serves and dials over TLS; with enable_mtls it also requires client
// certificates and checks that each message's sender matches its certificate.
func newTransport(cfg internal.Config, logger forge.Logger) (internal.Transport, error) {
	tlsConfig, err := transportTLS(cfg.Security, logger)
	if err != nil {
		return nil, err
	}

	requireIdentity := cfg.Security.EnableMTLS

	switch cfg.Transport.Type {
	case "tcp":
		return transport.NewTCPTransport(transport.TCPTransportConfig{
			NodeID:              cfg.NodeID,
			Address:             cfg.BindAddr,
			Port:                cfg.BindPort,
			ConnectionTimeout:   cfg.Transport.ConnectionTimeout,
			ReadTimeout:         cfg.Transport.Timeout,
			WriteTimeout:        cfg.Transport.Timeout,
			IdleTimeout:         cfg.Transport.IdleTimeout,
			KeepAlive:           cfg.Transport.KeepAliveInterval,
			MaxMessageSize:      cfg.Transport.MaxMessageSize,
			MaxConnections:      cfg.Transport.MaxConnections,
			TLSConfig:           tlsConfig,
			RequirePeerIdentity: requireIdentity,
		}, logger), nil
	case "http":
		return transport.NewHTTPTransport(transport.HTTPTransportConfig{
			NodeID:              cfg.NodeID,
			Address:             cfg.BindAddr,
			Port:                cfg.BindPort,
			RequestTimeout:      cfg.Transport.Timeout,
			IdleConnTimeout:     cfg.Transport.IdleTimeout,
			MaxMessageSize:      cfg.Transport.MaxMessageSize,
			MaxConnections:      cfg.Transport.MaxConnections,
			TLSConfig:           tlsConfig,
			RequirePeerIdentity: requireIdentity,
		}, logger), nil
	case "grpc":
		return transport.NewGRPCTransport(transport.GRPCTransportConfig{
			NodeID:              cfg.NodeID,
			Address:             cfg.BindAddr,
			Port:                cfg.BindPort,
			MaxConnections:      cfg.Transport.MaxConnections,
			ConnectionTimeout:   cfg.Transport.ConnectionTimeout,
			RequestTimeout:      cfg.Transport.Timeout,
			KeepAliveTime:       cfg.Transport.KeepAliveInterval,
			KeepAliveTimeout:    cfg.Transport.KeepAliveTimeout,
			IdleTimeout:         cfg.Transport.IdleTimeout,
			MaxMessageSize:      cfg.Transport.MaxMessageSize,
			TLSConfig:           tlsConfig,
			RequirePeerIdentity: requireIdentity,
		}, logger), nil
	default:
		return nil, fmt.Errorf("%w: transport type %q is not implemented; use tcp, http or grpc",
			internal.ErrInvalidConfig, cfg.Transport.Type)
	}
}

// transportTLS builds the transport TLS config with the transport package's
// TLSManager, or returns nil when TLS is off.
func transportTLS(sec internal.SecurityConfig, logger forge.Logger) (*tls.Config, error) {
	if !sec.EnableTLS && !sec.EnableMTLS {
		return nil, nil //nolint:nilnil // nil config means plain TCP
	}

	cfg := transport.DefaultTLSConfig()
	cfg.CertFile = sec.CertFile
	cfg.KeyFile = sec.KeyFile
	cfg.CAFile = sec.CAFile
	cfg.InsecureSkipVerify = sec.SkipVerify

	if sec.EnableMTLS {
		cfg.RequireClientCert = true
		cfg.ClientCAFile = sec.CAFile
	}

	manager, err := transport.NewTLSManager(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to load transport TLS settings: %w", err)
	}

	return manager.GetTLSConfig(), nil
}

// raftConfig maps the extension configuration onto the raft node.
func raftConfig(cfg internal.Config) raft.Config {
	// JSON base64-encodes snapshot data, so half the message limit leaves room
	// for the encoding and the envelope.
	chunk := cfg.Transport.MaxMessageSize / 2

	return raft.Config{
		NodeID:             cfg.NodeID,
		ClusterID:          cfg.ClusterID,
		Address:            cfg.BindAddr,
		Port:               cfg.BindPort,
		Join:               cfg.Join,
		HeartbeatInterval:  cfg.Raft.HeartbeatInterval,
		ElectionTimeoutMin: cfg.Raft.ElectionTimeoutMin,
		ElectionTimeoutMax: cfg.Raft.ElectionTimeoutMax,
		SnapshotInterval:   cfg.Raft.SnapshotInterval,
		SnapshotThreshold:  cfg.Raft.SnapshotThreshold,
		EnableSnapshots:    cfg.Advanced.EnableAutoSnapshot && cfg.Raft.SnapshotInterval > 0,
		SnapshotChunkSize:  chunk,
		MaxAppendEntries:   cfg.Raft.MaxAppendEntries,
		LogCacheSize:       cfg.Raft.LogCacheSize,
		TrailingLogs:       cfg.Raft.TrailingLogs,
		PreVoteEnabled:     cfg.Raft.PreVote,
		CheckQuorum:        cfg.Raft.CheckQuorum,
	}
}

// clusterCounts returns the number of members in the current configuration
// and how many of them this node currently knows to be reachable.
func (s *Service) clusterCounts(node *raft.Node) (total, active int) {
	if node == nil {
		return len(s.config.Peers), 0
	}

	nodes := s.clusterNodes(node)

	for _, n := range nodes {
		if n.Status == StatusActive {
			active++
		}
	}

	return len(nodes), active
}

// clusterNodes describes every member of the current raft configuration,
// learners included. A member counts as active when the leader heard from it
// within the election timeout; followers cannot observe each other, so on a
// follower only itself and the current leader are reported active.
func (s *Service) clusterNodes(node *raft.Node) []NodeInfo {
	stats := node.GetStats()
	window := s.config.Raft.ElectionTimeoutMax
	contacts := make(map[string]time.Time)

	for _, p := range node.Peers() {
		contacts[p.ID] = p.LastContact
	}

	cfg := node.Configuration()
	nodes := make([]NodeInfo, 0, len(cfg.Members))

	for _, m := range cfg.Members {
		info := NodeInfo{
			ID: m.ID, Address: m.Address, Port: m.Port,
			Role: RoleFollower, Status: StatusInactive,
			Metadata: map[string]any{"voter": m.Voter},
		}

		switch {
		case m.ID == s.config.NodeID:
			info.Role, info.Status, info.Term = stats.Role, StatusActive, stats.Term
		case m.ID == stats.Leader:
			info.Role, info.Status = RoleLeader, StatusActive
		case stats.Role == RoleLeader && time.Since(contacts[m.ID]) <= window:
			info.Status = StatusActive
		}

		nodes = append(nodes, info)
	}

	return nodes
}
