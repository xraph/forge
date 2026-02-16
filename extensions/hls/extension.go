package hls

import (
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus"
	"github.com/xraph/forge/extensions/hls/internal/distributed"
	"github.com/xraph/forge/extensions/hls/storage"
	forgestorage "github.com/xraph/forge/extensions/storage"
	"github.com/xraph/vessel"
)

// Extension implements forge.Extension for HLS functionality
type Extension struct {
	*forge.BaseExtension
	config      Config
	manager     HLS
	coordinator *distributed.Coordinator
}

// NewExtension creates a new HLS extension
func NewExtension(opts ...ConfigOption) forge.Extension {
	config := DefaultConfig()
	for _, opt := range opts {
		opt(&config)
	}

	base := forge.NewBaseExtension("hls", "2.0.0", "HTTP Live Streaming with adaptive bitrate, transcoding, and distributed support")
	return &Extension{
		BaseExtension: base,
		config:        config,
	}
}

// NewExtensionWithConfig creates a new HLS extension with a complete config
func NewExtensionWithConfig(config Config) forge.Extension {
	return NewExtension(WithConfig(config))
}

// Register registers the HLS extension with the app
func (e *Extension) Register(app forge.App) error {
	// Call base registration
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	// Load config from ConfigManager
	programmaticConfig := e.config
	finalConfig := DefaultConfig()
	if err := e.LoadConfig("hls", &finalConfig, programmaticConfig, DefaultConfig(), programmaticConfig.RequireConfig); err != nil {
		if programmaticConfig.RequireConfig {
			return fmt.Errorf("hls: failed to load required config: %w", err)
		}
		e.Logger().Warn("hls: using default/programmatic config",
			forge.F("error", err.Error()),
		)
	}
	e.config = finalConfig

	// Validate config
	if err := e.config.Validate(); err != nil {
		return fmt.Errorf("hls config validation failed: %w", err)
	}

	// Generate node ID if not set
	if e.config.EnableDistributed && e.config.NodeID == "" {
		e.config.NodeID = uuid.New().String()
	}

	// Get storage manager from DI
	storageMgr, err := forge.Inject[*forgestorage.StorageManager](app.Container())
	if err != nil {
		return fmt.Errorf("failed to resolve storage manager (ensure storage extension is registered): %w", err)
	}

	// Get the configured backend or use default
	var forgeStorage forgestorage.Storage
	if e.config.StorageBackend != "" && e.config.StorageBackend != "default" {
		forgeStorage = storageMgr.Backend(e.config.StorageBackend)
		if forgeStorage == nil {
			return fmt.Errorf("storage backend %s not found", e.config.StorageBackend)
		}
	} else {
		forgeStorage = storageMgr // Use default backend
	}

	// Wrap with HLS-specific storage
	hlsStore := storage.NewHLSStorage(forgeStorage, e.config.StoragePrefix)

	// Initialize distributed coordinator if enabled
	var coordinator *distributed.Coordinator
	if e.config.EnableDistributed {
		consensusSvc, err := forge.Inject[consensus.ConsensusService](app.Container())
		if err != nil {
			return fmt.Errorf("distributed mode requires consensus extension: %w", err)
		}

		coordinator = distributed.NewCoordinator(e.config.NodeID, consensusSvc, e.Logger())

		// Integrate state machine with consensus
		if err := coordinator.IntegrateWithConsensus(); err != nil {
			e.Logger().Warn("state machine integration incomplete",
				forge.F("error", err),
				forge.F("note", "distributed mode will work with reduced functionality"),
			)
		}

		// Start coordinator
		if err := coordinator.Start(); err != nil {
			return fmt.Errorf("failed to start distributed coordinator: %w", err)
		}
		e.coordinator = coordinator

		e.Logger().Info("distributed mode enabled",
			forge.F("node_id", e.config.NodeID),
			forge.F("cluster_id", e.config.ClusterID),
		)
	}

	// Create HLS manager
	manager, err := NewManager(e.config, hlsStore)
	if err != nil {
		return fmt.Errorf("failed to create HLS manager: %w", err)
	}
	e.manager = manager

	// Register HLS service in DI container
	hlsManager := e.manager
	if err := vessel.ProvideConstructor(app.Container(), func() HLS {
		return hlsManager
	}, vessel.WithAliases(ServiceKey)); err != nil {
		return fmt.Errorf("failed to register hls service: %w", err)
	}

	// Register HTTP routes
	router := app.Router()

	// Distributed middleware is available via e.coordinator if needed
	// Leadership checks can be done in handlers using coordinator.IsLeader()
	// Cluster headers can be added using coordinator's helper methods

	// Master playlist endpoint
	router.GET(e.config.BasePath+"/:streamID/master.m3u8", e.handleMasterPlaylist,
		forge.WithName("hls-master-playlist"),
		forge.WithTags("hls", "streaming"),
		forge.WithSummary("Get HLS master playlist"),
	)

	// Media playlist endpoint
	router.GET(e.config.BasePath+"/:streamID/variants/:variantID/playlist.m3u8", e.handleMediaPlaylist,
		forge.WithName("hls-media-playlist"),
		forge.WithTags("hls", "streaming"),
		forge.WithSummary("Get HLS media playlist"),
	)

	// Segment endpoint
	router.GET(e.config.BasePath+"/:streamID/variants/:variantID/segment_:segmentNum.ts", e.handleSegment,
		forge.WithName("hls-segment"),
		forge.WithTags("hls", "streaming"),
		forge.WithSummary("Get HLS segment"),
	)

	// Stream management endpoints
	router.POST(e.config.BasePath+"/streams", e.handleCreateStream,
		forge.WithName("hls-create-stream"),
		forge.WithTags("hls", "streaming", "management"),
		forge.WithSummary("Create a new HLS stream"),
	)

	router.GET(e.config.BasePath+"/streams/:streamID", e.handleGetStream,
		forge.WithName("hls-get-stream"),
		forge.WithTags("hls", "streaming", "management"),
		forge.WithSummary("Get HLS stream details"),
	)

	router.DELETE(e.config.BasePath+"/streams/:streamID", e.handleDeleteStream,
		forge.WithName("hls-delete-stream"),
		forge.WithTags("hls", "streaming", "management"),
		forge.WithSummary("Delete an HLS stream"),
	)

	router.GET(e.config.BasePath+"/streams", e.handleListStreams,
		forge.WithName("hls-list-streams"),
		forge.WithTags("hls", "streaming", "management"),
		forge.WithSummary("List all HLS streams"),
	)

	// Live streaming endpoints
	router.POST(e.config.BasePath+"/streams/:streamID/start", e.handleStartLiveStream,
		forge.WithName("hls-start-live"),
		forge.WithTags("hls", "streaming", "live"),
		forge.WithSummary("Start a live stream"),
	)

	router.POST(e.config.BasePath+"/streams/:streamID/stop", e.handleStopLiveStream,
		forge.WithName("hls-stop-live"),
		forge.WithTags("hls", "streaming", "live"),
		forge.WithSummary("Stop a live stream"),
	)

	router.POST(e.config.BasePath+"/streams/:streamID/ingest", e.handleIngestSegment,
		forge.WithName("hls-ingest-segment"),
		forge.WithTags("hls", "streaming", "live"),
		forge.WithSummary("Ingest a segment for live streaming"),
	)

	// Stats endpoint
	router.GET(e.config.BasePath+"/streams/:streamID/stats", e.handleGetStats,
		forge.WithName("hls-stream-stats"),
		forge.WithTags("hls", "streaming", "stats"),
		forge.WithSummary("Get stream statistics"),
	)

	e.Logger().Info("hls extension registered",
		forge.F("base_path", e.config.BasePath),
		forge.F("storage_backend", e.config.StorageBackend),
		forge.F("storage_prefix", e.config.StoragePrefix),
		forge.F("transcoding_enabled", e.config.EnableTranscoding),
		forge.F("distributed", e.config.EnableDistributed),
	)

	return nil
}

// Start starts the HLS extension
func (e *Extension) Start(ctx context.Context) error {
	e.Logger().Info("starting hls extension")
	return nil
}

// Stop stops the HLS extension
func (e *Extension) Stop(ctx context.Context) error {
	e.Logger().Info("stopping hls extension")

	// Stop distributed coordinator if enabled
	if e.coordinator != nil {
		if err := e.coordinator.Stop(); err != nil {
			e.Logger().Error("failed to stop distributed coordinator",
				forge.F("error", err),
			)
		}
	}

	// Stop manager
	if manager, ok := e.manager.(*Manager); ok {
		manager.Stop()
	}

	return nil
}

// Health returns the health status of the HLS extension
func (e *Extension) Health(ctx context.Context) error {
	// TODO: Implement proper health checks
	return nil
}

// HTTP Handlers

func (e *Extension) handleMasterPlaylist(ctx forge.Context) error {
	streamID := ctx.Param("streamID")

	playlist, err := e.manager.GetMasterPlaylist(ctx.Request().Context(), streamID)
	if err != nil {
		return forge.NewHTTPError(404, fmt.Sprintf("stream not found: %v", err))
	}

	ctx.Response().Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	if e.config.EnableCORS {
		e.setCORSHeaders(ctx)
	}

	return ctx.String(200, playlist.Content)
}

func (e *Extension) handleMediaPlaylist(ctx forge.Context) error {
	streamID := ctx.Param("streamID")
	variantID := ctx.Param("variantID")

	playlist, err := e.manager.GetMediaPlaylist(ctx.Request().Context(), streamID, variantID)
	if err != nil {
		return forge.NewHTTPError(404, fmt.Sprintf("playlist not found: %v", err))
	}

	ctx.Response().Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	if e.config.EnableCORS {
		e.setCORSHeaders(ctx)
	}

	return ctx.String(200, playlist.Content)
}

func (e *Extension) handleSegment(ctx forge.Context) error {
	streamID := ctx.Param("streamID")
	variantID := ctx.Param("variantID")
	segmentNum := 0
	fmt.Sscanf(ctx.Param("segmentNum"), "%d", &segmentNum)

	// Get segment from manager
	reader, err := e.manager.GetSegment(ctx.Request().Context(), streamID, variantID, segmentNum)
	if err != nil {
		return forge.NewHTTPError(404, fmt.Sprintf("segment not found: %v", err))
	}
	defer reader.Close()

	// Set headers
	ctx.Response().Header().Set("Content-Type", "video/MP2T")
	ctx.Response().Header().Set("Cache-Control", "max-age=3600")
	if e.config.EnableCORS {
		e.setCORSHeaders(ctx)
	}

	// Stream segment to response
	ctx.Response().WriteHeader(200)
	if _, err := io.Copy(ctx.Response(), reader); err != nil {
		e.Logger().Error("failed to stream segment",
			forge.F("stream_id", streamID),
			forge.F("variant_id", variantID),
			forge.F("segment_num", segmentNum),
			forge.F("error", err.Error()),
		)
		return err
	}

	return nil
}

func (e *Extension) handleCreateStream(ctx forge.Context) error {
	var opts StreamOptions
	if err := ctx.BindJSON(&opts); err != nil {
		return forge.BadRequest(fmt.Sprintf("invalid request: %v", err))
	}

	stream, err := e.manager.CreateStream(ctx.Request().Context(), opts)
	if err != nil {
		return forge.InternalError(fmt.Errorf("failed to create stream: %w", err))
	}

	return ctx.JSON(201, stream)
}

func (e *Extension) handleGetStream(ctx forge.Context) error {
	streamID := ctx.Param("streamID")

	stream, err := e.manager.GetStream(ctx.Request().Context(), streamID)
	if err != nil {
		return forge.NotFound(fmt.Sprintf("stream not found: %v", err))
	}

	return ctx.JSON(200, stream)
}

func (e *Extension) handleDeleteStream(ctx forge.Context) error {
	streamID := ctx.Param("streamID")

	if err := e.manager.DeleteStream(ctx.Request().Context(), streamID); err != nil {
		return forge.InternalError(fmt.Errorf("failed to delete stream: %w", err))
	}

	return ctx.NoContent(204)
}

func (e *Extension) handleListStreams(ctx forge.Context) error {
	streams, err := e.manager.ListStreams(ctx.Request().Context())
	if err != nil {
		return forge.InternalError(fmt.Errorf("failed to list streams: %w", err))
	}

	return ctx.JSON(200, map[string]interface{}{
		"streams": streams,
		"count":   len(streams),
	})
}

func (e *Extension) handleStartLiveStream(ctx forge.Context) error {
	streamID := ctx.Param("streamID")

	if err := e.manager.StartLiveStream(ctx.Request().Context(), streamID); err != nil {
		return forge.InternalError(fmt.Errorf("failed to start stream: %w", err))
	}

	return ctx.JSON(200, map[string]string{
		"status": "started",
	})
}

func (e *Extension) handleStopLiveStream(ctx forge.Context) error {
	streamID := ctx.Param("streamID")

	if err := e.manager.StopLiveStream(ctx.Request().Context(), streamID); err != nil {
		return forge.InternalError(fmt.Errorf("failed to stop stream: %w", err))
	}

	return ctx.JSON(200, map[string]string{
		"status": "stopped",
	})
}

func (e *Extension) handleIngestSegment(ctx forge.Context) error {
	streamID := ctx.Param("streamID")

	var segment Segment
	if err := ctx.BindJSON(&segment); err != nil {
		return forge.BadRequest(fmt.Sprintf("invalid request: %v", err))
	}

	segment.StreamID = streamID

	if err := e.manager.IngestSegment(ctx.Request().Context(), streamID, &segment); err != nil {
		return forge.InternalError(fmt.Errorf("failed to ingest segment: %w", err))
	}

	return ctx.JSON(200, map[string]string{
		"status": "ingested",
	})
}

func (e *Extension) handleGetStats(ctx forge.Context) error {
	streamID := ctx.Param("streamID")

	stats, err := e.manager.GetStreamStats(ctx.Request().Context(), streamID)
	if err != nil {
		return forge.NotFound(fmt.Sprintf("stats not found: %v", err))
	}

	return ctx.JSON(200, stats)
}

func (e *Extension) setCORSHeaders(ctx forge.Context) {
	if len(e.config.AllowedOrigins) > 0 {
		origin := e.config.AllowedOrigins[0]
		if origin == "*" {
			ctx.Response().Header().Set("Access-Control-Allow-Origin", "*")
		} else {
			ctx.Response().Header().Set("Access-Control-Allow-Origin", origin)
		}
	}
	ctx.Response().Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	ctx.Response().Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
}
