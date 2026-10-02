package main

import (
	"context"
	"log"
	"os"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/hls"
	"github.com/xraph/trove"
	"github.com/xraph/trove/drivers/localdriver"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Usage: vod <video_file>")
	}

	videoFile := os.Args[1]

	// Create Forge app
	app := forge.New(
		forge.WithAppName("hls-vod-demo"),
		forge.WithAppVersion("1.0.0"),
	)

	// Open the object store
	store := provideStore(app, "local://./vod_storage")
	defer store.Close(context.Background())

	// Configure HLS extension
	hlsExt := hls.NewExtension(
		hls.WithBasePath("/hls"),
		hls.WithBaseURL("http://localhost:8080/hls"),
		hls.WithStorageBackend("default"),
		hls.WithTargetDuration(10),
		hls.WithTranscoding(true,
			hls.Profile360p,
			hls.Profile480p,
			hls.Profile720p,
			hls.Profile1080p,
		),
		hls.WithFFmpegPaths("ffmpeg", "ffprobe"),
		hls.WithCORS(true, "*"),
	)

	// Register extensions
	app.RegisterExtension(hlsExt)

	// Start app
	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		log.Fatalf("Failed to start app: %v", err)
	}

	// Get HLS service from DI
	hlsSvc, err := forge.Inject[hls.HLS](app.Container())
	if err != nil {
		log.Fatalf("Failed to resolve HLS service: %v", err)
	}

	// Create VOD stream from file
	log.Printf("Creating VOD stream from: %s", videoFile)

	stream, err := hlsSvc.(*hls.Manager).CreateVODFromFile(ctx, videoFile, hls.VODOptions{
		Title:          "VOD Example",
		Description:    "Video on demand example",
		TargetDuration: 10,
		TranscodeProfiles: []hls.TranscodeProfile{
			hls.Profile360p,
			hls.Profile480p,
			hls.Profile720p,
			hls.Profile1080p,
		},
	})
	if err != nil {
		log.Fatalf("Failed to create VOD: %v", err)
	}

	log.Printf("VOD stream created: %s", stream.ID)
	log.Printf("Master playlist: http://localhost:8080/hls/%s/master.m3u8", stream.ID)
	log.Println("Note: Transcoding is async. Segments will be available once transcoding completes.")

	// Start HTTP server
	go func() {
		if err := app.Run(); err != nil {
			log.Printf("Server error: %v", err)
		}
	}()

	log.Println("VOD server started on http://localhost:8080")
	log.Println("Press Ctrl+C to exit")

	// Wait indefinitely
	select {}
}

// provideStore opens a local object store at dsn and adds it to the
// container, where the HLS extension resolves it as its default backend.
// Registering trove's own extension provides the same *trove.Trove.
func provideStore(app forge.App, dsn string) *trove.Trove {
	drv := localdriver.New()
	if err := drv.Open(context.Background(), dsn); err != nil {
		log.Fatalf("Failed to open storage driver: %v", err)
	}

	store, err := trove.Open(drv)
	if err != nil {
		log.Fatalf("Failed to open storage: %v", err)
	}

	if err := forge.ProvideValue(app.Container(), store); err != nil {
		log.Fatalf("Failed to provide storage: %v", err)
	}

	return store
}
