package main

import (
	"context"
	"log"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/auth"
	"github.com/xraph/forge/extensions/streaming"
	"github.com/xraph/forge/extensions/webrtc"
)

func main() {
	// Create Forge application
	app := forge.NewApp(forge.AppConfig{
		Name:    "WebRTC Video Call App",
		Version: "1.0.0",
	})

	// Configure authentication
	authConfig := auth.Config{
		Providers: []string{"jwt"},
		// JWT configuration would go here
	}
	authExt := auth.New(authConfig)
	app.Use(authExt)

	// Configure streaming extension
	streamingConfig := streaming.Config{
		RequireAuth:   true,
		AuthProviders: []string{"jwt"},

		// Distributed coordination for multi-node setup
		Coordination: streaming.CoordinationConfig{
			Enabled:       true,
			Backend:       "redis",
			URLs:          []string{"redis://localhost:6379"},
			PresenceSync:  true,
			RoomStateSync: true,
			SyncInterval:  5 * time.Second,
		},

		// Rate limiting
		RateLimitEnabled: true,
		RateLimit: streaming.RateLimitConfig{
			MessagesPerSecond:  10,
			MessagesPerMinute:  100,
			ConnectionsPerUser: 5,
		},
	}
	streamingExt := streaming.New(streamingConfig)
	app.Use(streamingExt)

	// Configure WebRTC extension
	webrtcConfig := webrtc.Config{
		// Signaling
		SignalingEnabled: true,
		SignalingTimeout: 30 * time.Second,

		// Topology - use SFU for scalability
		Topology: webrtc.TopologySFU,

		// STUN servers for NAT traversal
		STUNServers: []string{
			"stun:stun.l.google.com:19302",
			"stun:stun1.l.google.com:19302",
		},

		// TURN servers for relay (when P2P fails)
		TURNServers: []webrtc.TURNConfig{
			{
				URLs:       []string{"turn:turn.example.com:3478"},
				Username:   "turnuser",
				Credential: "turnpass",
			},
		},

		// Media configuration
		MediaConfig: webrtc.MediaConfig{
			AudioEnabled:        true,
			AudioCodecs:         []string{"opus"},
			VideoEnabled:        true,
			VideoCodecs:         []string{"VP8", "H264"},
			MaxAudioBitrate:     128,
			MaxVideoBitrate:     2500,
			MinVideoBitrate:     150,
			MaxWidth:            1920,
			MaxHeight:           1080,
			MaxFPS:              30,
			DataChannelsEnabled: true,
		},

		// SFU configuration
		SFUConfig: &webrtc.SFUConfig{
			WorkerCount:      4,
			MaxBandwidthMbps: 100,
			AdaptiveBitrate:  true,
			SimulcastEnabled: true,
			QualityLayers: []webrtc.QualityLayer{
				{RID: "f", MaxWidth: 1920, MaxHeight: 1080, MaxFPS: 30, Bitrate: 2500},
				{RID: "h", MaxWidth: 1280, MaxHeight: 720, MaxFPS: 30, Bitrate: 1200},
				{RID: "q", MaxWidth: 640, MaxHeight: 360, MaxFPS: 30, Bitrate: 500},
			},
		},

		// Quality monitoring
		QualityConfig: webrtc.QualityConfig{
			MonitorEnabled:       true,
			MonitorInterval:      5 * time.Second,
			MaxPacketLoss:        5.0,
			MaxJitter:            30 * time.Millisecond,
			MinBitrate:           100,
			AdaptiveQuality:      true,
			QualityCheckInterval: 10 * time.Second,
		},

		// Recording
		RecordingEnabled: true,
		RecordingPath:    "./recordings",

		// Security
		RequireAuth: true,
		AllowGuests: false,
	}

	webrtcExt, err := webrtc.New(streamingExt, webrtcConfig)
	if err != nil {
		log.Fatalf("Failed to create WebRTC extension: %v", err)
	}
	app.Use(webrtcExt)

	// Setup routes
	router := app.Router()

	// Register WebRTC signaling WebSocket endpoint
	webrtcExt.RegisterRoutes(router)

	// Create call room
	router.POST("/call/create", func(ctx forge.Context) error {
		roomID := ctx.PostValue("room_id")
		roomName := ctx.PostValue("room_name")
		maxMembers := ctx.PostValueInt("max_members", 10)

		room, err := webrtcExt.CreateCallRoom(ctx.Request().Context(), roomID, streaming.RoomOptions{
			Name:       roomName,
			MaxMembers: maxMembers,
		})
		if err != nil {
			return ctx.Status(400).JSON(map[string]string{
				"error": err.Error(),
			})
		}

		return ctx.JSON(map[string]any{
			"room_id":   room.ID(),
			"room_name": room.Name(),
			"status":    "created",
		})
	})

	// Get call room info
	router.GET("/call/{roomID}", func(ctx forge.Context) error {
		roomID := ctx.Param("roomID")

		room, err := webrtcExt.GetCallRoom(roomID)
		if err != nil {
			return ctx.Status(404).JSON(map[string]string{
				"error": "room not found",
			})
		}

		participants := room.GetParticipants()

		return ctx.JSON(map[string]any{
			"room_id":      room.ID(),
			"room_name":    room.Name(),
			"participants": participants,
		})
	})

	// Join call
	router.POST("/call/{roomID}/join", func(ctx forge.Context) error {
		roomID := ctx.Param("roomID")
		userID := ctx.Get("user_id").(string)
		displayName := ctx.PostValue("display_name")

		peer, err := webrtcExt.JoinCall(
			ctx.Request().Context(),
			roomID,
			userID,
			&webrtc.JoinOptions{
				AudioEnabled: true,
				VideoEnabled: true,
				DisplayName:  displayName,
			},
		)
		if err != nil {
			return ctx.Status(400).JSON(map[string]string{
				"error": err.Error(),
			})
		}

		return ctx.JSON(map[string]any{
			"peer_id": peer.ID(),
			"user_id": userID,
			"status":  "joined",
		})
	})

	// Leave call
	router.POST("/call/{roomID}/leave", func(ctx forge.Context) error {
		roomID := ctx.Param("roomID")
		userID := ctx.Get("user_id").(string)

		err := webrtcExt.LeaveCall(ctx.Request().Context(), roomID, userID)
		if err != nil {
			return ctx.Status(400).JSON(map[string]string{
				"error": err.Error(),
			})
		}

		return ctx.JSON(map[string]any{
			"status": "left",
		})
	})

	// Get call quality metrics
	router.GET("/call/{roomID}/quality", func(ctx forge.Context) error {
		roomID := ctx.Param("roomID")

		room, err := webrtcExt.GetCallRoom(roomID)
		if err != nil {
			return ctx.Status(404).JSON(map[string]string{
				"error": "room not found",
			})
		}

		quality, err := room.GetQuality(ctx.Request().Context())
		if err != nil {
			return ctx.Status(500).JSON(map[string]string{
				"error": err.Error(),
			})
		}

		return ctx.JSON(quality)
	})

	// Start recording
	router.POST("/call/{roomID}/record/start", func(ctx forge.Context) error {
		roomID := ctx.Param("roomID")

		recorder := webrtcExt.GetRecorder()
		if recorder == nil {
			return ctx.Status(400).JSON(map[string]string{
				"error": "recording not enabled",
			})
		}

		err := recorder.Start(ctx.Request().Context(), roomID, &webrtc.RecordingOptions{
			Format:     "webm",
			VideoCodec: "VP8",
			AudioCodec: "opus",
			OutputPath: "./recordings/" + roomID + ".webm",
		})
		if err != nil {
			return ctx.Status(500).JSON(map[string]string{
				"error": err.Error(),
			})
		}

		return ctx.JSON(map[string]any{
			"status": "recording_started",
		})
	})

	// Stop recording
	router.POST("/call/{roomID}/record/stop", func(ctx forge.Context) error {
		roomID := ctx.Param("roomID")

		recorder := webrtcExt.GetRecorder()
		err := recorder.Stop(ctx.Request().Context(), roomID)
		if err != nil {
			return ctx.Status(500).JSON(map[string]string{
				"error": err.Error(),
			})
		}

		return ctx.JSON(map[string]any{
			"status": "recording_stopped",
		})
	})

	// List all active calls
	router.GET("/calls", func(ctx forge.Context) error {
		rooms := webrtcExt.GetCallRooms()

		roomList := make([]map[string]any, 0, len(rooms))
		for _, room := range rooms {
			roomList = append(roomList, map[string]any{
				"room_id":           room.ID(),
				"room_name":         room.Name(),
				"participant_count": len(room.GetParticipants()),
			})
		}

		return ctx.JSON(map[string]any{
			"rooms": roomList,
			"total": len(roomList),
		})
	})

	// Health check
	router.GET("/health", func(ctx forge.Context) error {
		if err := webrtcExt.Health(context.Background()); err != nil {
			return ctx.Status(503).JSON(map[string]string{
				"status": "unhealthy",
				"error":  err.Error(),
			})
		}

		return ctx.JSON(map[string]any{
			"status": "healthy",
			"webrtc": map[string]any{
				"topology":     webrtcConfig.Topology,
				"stun_servers": len(webrtcConfig.STUNServers),
				"turn_servers": len(webrtcConfig.TURNServers),
			},
		})
	})

	// Run server
	log.Printf("Starting WebRTC server on :8080")
	if err := app.Run(":8080"); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
