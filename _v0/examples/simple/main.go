package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/xraph/forge/v0"
	"github.com/xraph/forge/v0/pkg/logger"
)

func main() {
	// Create a simple Forge application
	app, err := forge.NewSimpleApplicationWithOptions(
		"simple-example",
		"1.0.0",
		forge.WithSimplePort(8080),
	)
	if err != nil {
		log.Fatalf("Failed to create application: %v", err)
	}

	// Add a simple route
	app.Router().GET("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"message": "Hello from Simple Forge!", "app": "%s", "version": "%s"}`,
			app.Name(), app.Version())
	})

	app.Router().GET("/health", func(w http.ResponseWriter, r *http.Request) {
		if err := app.HealthCheck(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, `{"status": "unhealthy", "error": "%s"}`, err.Error())
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status": "healthy", "uptime": "%s"}`, time.Since(app.GetInfo().StartTime))
	})

	// Start the application
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := app.Start(ctx); err != nil {
		log.Fatalf("Failed to start application: %v", err)
	}

	// Set up graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	app.Logger().Info("Simple Forge application running",
		logger.String("name", app.Name()),
		logger.String("version", app.Version()),
		logger.String("port", "8080"),
	)

	// Wait for shutdown signal
	<-sigChan
	app.Logger().Info("Received shutdown signal, stopping application...")

	// Stop the application
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := app.Stop(shutdownCtx); err != nil {
		log.Printf("Error during shutdown: %v", err)
		os.Exit(1)
	}

	app.Logger().Info("Application stopped successfully")
}
