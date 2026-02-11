package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/hls"
	"github.com/xraph/forge/extensions/storage"
)

func main() {
	// Create Forge app
	app := forge.New(
		forge.WithAppName("hls-demo"),
		forge.WithAppVersion("1.0.0"),
	)

	// Configure storage extension (required by HLS)
	storageExt := storage.NewExtensionWithConfig(storage.Config{
		Backends: map[string]storage.BackendConfig{
			"default": {
				Type: "local",
				Config: map[string]interface{}{
					"base_path": "./hls_storage",
				},
			},
		},
		Default:             "default",
		UseEnhancedBackend:  true,
		EnablePresignedURLs: false,
	})

	// Configure HLS extension
	hlsExt := hls.NewExtension(
		hls.WithBasePath("/hls"),
		hls.WithBaseURL("http://localhost:8080/hls"),
		hls.WithStorageBackend("default"),
		hls.WithStoragePrefix("hls"),
		hls.WithTargetDuration(6),
		hls.WithDVRWindow(10),
		hls.WithTranscoding(true, hls.DefaultProfiles()...),
		hls.WithFFmpegPaths("ffmpeg", "ffprobe"),
		hls.WithCORS(true, "*"),
		hls.WithCleanup(24*time.Hour, 1*time.Hour),
	)

	// Register extensions
	app.RegisterExtension(storageExt)
	app.RegisterExtension(hlsExt)

	// Start app
	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		log.Fatalf("Failed to start app: %v", err)
	}

	// Create a demo live stream
	createDemoStream(app)

	// Start HTTP server
	go func() {
		if err := app.Run(); err != nil {
			log.Printf("Server error: %v", err)
		}
	}()

	log.Println("HLS server started on http://localhost:8080")
	log.Println("API endpoints:")
	log.Println("  POST   /hls/streams              - Create stream")
	log.Println("  GET    /hls/streams              - List streams")
	log.Println("  GET    /hls/streams/:id          - Get stream details")
	log.Println("  DELETE /hls/streams/:id          - Delete stream")
	log.Println("  POST   /hls/streams/:id/start    - Start live stream")
	log.Println("  POST   /hls/streams/:id/stop     - Stop live stream")
	log.Println("  POST   /hls/streams/:id/ingest   - Ingest segment")
	log.Println("  GET    /hls/:id/master.m3u8      - Master playlist")
	log.Println("  GET    /hls/:id/variants/:vid/playlist.m3u8 - Media playlist")

	// Wait for interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down...")
	if err := app.Stop(ctx); err != nil {
		log.Printf("Error stopping app: %v", err)
	}
}

func createDemoStream(app forge.App) {
	// Get HLS service from DI
	hlsSvc, err := forge.Resolve[hls.HLS](app.Container(), "hls")
	if err != nil {
		log.Printf("Failed to resolve HLS service: %v", err)
		return
	}

	// Create a demo live stream
	ctx := context.Background()
	stream, err := hlsSvc.CreateStream(ctx, hls.StreamOptions{
		Title:          "Demo Live Stream",
		Description:    "Example HLS live stream",
		Type:           hls.StreamTypeLive,
		TargetDuration: 6,
		DVRWindowSize:  10,
		TranscodeProfiles: []hls.TranscodeProfile{
			hls.Profile360p,
			hls.Profile720p,
		},
	})
	if err != nil {
		log.Printf("Failed to create demo stream: %v", err)
		return
	}

	log.Printf("Created demo stream: %s", stream.ID)
	log.Printf("Master playlist: http://localhost:8080/hls/%s/master.m3u8", stream.ID)
}
