package main

import (
	"crypto/tls"
	"log"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/streaming"
)

func main() {
	// Create Forge application with config
	config := forge.DefaultAppConfig()
	config.Name = "webtransport-demo"
	config.Version = "1.0.0"
	config.HTTPAddress = ":8080"

	app := forge.NewApp(config)

	// Add streaming extension with WebTransport support
	if err := app.RegisterExtension(streaming.NewExtension(
		streaming.WithFeatures(true, true, true, true, true), // Enable all features
		streaming.WithLocalBackend(),
	)); err != nil {
		log.Fatal(err)
	}

	// Setup TLS (required for WebTransport)
	tlsConfig := &tls.Config{
		// In production, load real certificates
		// Certificates: []tls.Certificate{cert},
		MinVersion: tls.VersionTLS13,
	}
	_ = tlsConfig // Use in production

	// Get router
	router := app.Router()

	// Register a demo HTTP endpoint to show the app is running
	router.GET("/", func(ctx forge.Context) error {
		return ctx.JSON(200, map[string]interface{}{
			"message": "WebTransport Demo Server",
			"note":    "WebTransport support requires HTTP/3 implementation",
			"features": map[string]bool{
				"websocket": true,
				"sse":       true,
				"http":      true,
			},
		})
	})

	// Enable WebTransport support
	wtConfig := forge.DefaultWebTransportConfig()
	wtConfig.MaxBidiStreams = 200
	wtConfig.MaxUniStreams = 200
	wtConfig.EnableDatagrams = true

	if err := router.EnableWebTransport(wtConfig); err != nil {
		log.Fatal(err)
	}

	// Register WebTransport endpoint
	router.WebTransport("/wt/chat", handleWebTransportChat,
		forge.WithName("webtransport-chat"),
		forge.WithTags("chat", "webtransport"),
		forge.WithSummary("WebTransport chat endpoint"),
	)

	// Start HTTP/3 server for WebTransport in a goroutine
	go func() {
		if err := router.StartHTTP3(":4433", tlsConfig); err != nil {
			log.Printf("HTTP/3 server error: %v", err)
		}
	}()

	log.Println("WebTransport demo server starting...")
	log.Println("HTTP server: http://localhost:8080")
	log.Println("HTTP/3 (WebTransport) server: https://localhost:4433")
	log.Println("WebTransport endpoint: https://localhost:4433/wt/chat")
	log.Println("Press Ctrl+C to exit")

	// Start the application - it will block until shutdown
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

// WebTransport Handler Implementation

func handleWebTransportChat(ctx forge.Context, session forge.WebTransportSession) error {
	log.Printf("New WebTransport session: %s from %s", session.ID(), session.RemoteAddr())

	// Handle incoming streams
	go func() {
		for {
			stream, err := session.AcceptStream(ctx.Request().Context())
			if err != nil {
				log.Printf("Failed to accept stream: %v", err)
				return
			}

			go handleStream(stream)
		}
	}()

	// Handle incoming datagrams
	go func() {
		for {
			data, err := session.ReceiveDatagram(ctx.Request().Context())
			if err != nil {
				log.Printf("Failed to receive datagram: %v", err)
				return
			}

			handleDatagram(session, data)
		}
	}()

	// Send periodic pings via datagrams
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Request().Context().Done():
			return nil
		case <-ticker.C:
			if err := session.SendDatagram([]byte("ping")); err != nil {
				return err
			}
		}
	}
}

func handleStream(stream forge.WebTransportStream) {
	defer stream.Close()

	log.Printf("New stream opened")

	// Echo received data back
	buf := make([]byte, 4096)
	for {
		n, err := stream.Read(buf)
		if err != nil {
			log.Printf("Stream read error: %v", err)
			return
		}

		log.Printf("Received on stream: %s", string(buf[:n]))

		// Echo back
		if _, err := stream.Write(buf[:n]); err != nil {
			log.Printf("Stream write error: %v", err)
			return
		}
	}
}

func handleDatagram(session forge.WebTransportSession, data []byte) {
	log.Printf("Received datagram: %s", string(data))

	// Echo back
	response := append([]byte("echo: "), data...)
	if err := session.SendDatagram(response); err != nil {
		log.Printf("Failed to send datagram: %v", err)
	}
}
