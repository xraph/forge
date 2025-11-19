package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus"
	"github.com/xraph/forge/extensions/hls"
	"github.com/xraph/forge/extensions/storage"
)

var (
	nodeID   = flag.String("node", "", "Node ID (auto-generated if empty)")
	port     = flag.Int("port", 8080, "HTTP port")
	raftPort = flag.Int("raft", 7000, "Raft port")
	peers    = flag.String("peers", "", "Comma-separated list of peer addresses (host:port)")
)

func main() {
	flag.Parse()

	// Generate node ID if not provided
	if *nodeID == "" {
		*nodeID = fmt.Sprintf("hls-node-%s", uuid.New().String()[:8])
	}

	log.Printf("Starting HLS node: %s", *nodeID)
	log.Printf("HTTP Port: %d", *port)
	log.Printf("Raft Port: %d", *raftPort)

	// Create Forge app
	app := forge.New(
		forge.WithAppName("hls-distributed"),
		forge.WithAppVersion("1.0.0"),
	)

	// Configure storage extension (shared storage required for distributed mode)
	storageExt := storage.NewExtension(storage.Config{
		Backends: map[string]storage.BackendConfig{
			"default": {
				Type: "local",
				Config: map[string]interface{}{
					"base_path": fmt.Sprintf("./data/node-%s", *nodeID),
				},
			},
		},
		Default:            "default",
		UseEnhancedBackend: true,
	})

	// Configure consensus extension
	consensusExt := consensus.NewExtension(
		consensus.WithNodeID(*nodeID),
		consensus.WithClusterID("hls-cluster"),
		consensus.WithTransportType("tcp"),
		consensus.WithStorageType("boltdb"),
		consensus.WithStoragePath(fmt.Sprintf("./data/raft-%s", *nodeID)),
	)

	// Parse peers if provided
	if *peers != "" {
		log.Printf("Joining cluster with peers: %s", *peers)
		// Peers would be added here based on comma-separated list
	}

	// Configure HLS extension with distributed mode
	hlsExt := hls.NewExtension(
		hls.WithBasePath("/hls"),
		hls.WithBaseURL(fmt.Sprintf("http://localhost:%d/hls", *port)),
		hls.WithStorageBackend("default"),
		hls.WithTargetDuration(6),
		hls.WithDVRWindow(10),
		hls.WithTranscoding(false), // Disable transcoding for demo
		hls.WithDistributed(true),
		hls.WithNodeID(*nodeID),
		hls.WithClusterID("hls-cluster"),
		hls.WithFailover(true),
		hls.WithCORS(true, "*"),
	)

	// Register extensions
	app.RegisterExtension(storageExt)
	app.RegisterExtension(consensusExt) // Consensus must be registered before HLS
	app.RegisterExtension(hlsExt)

	// Start app
	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		log.Fatalf("Failed to start app: %v", err)
	}

	// Start HTTP server in background
	go func() {
		addr := fmt.Sprintf(":%d", *port)
		log.Printf("HLS server listening on %s", addr)
		log.Printf("API endpoints:")
		log.Printf("  POST   /hls/streams              - Create stream (leader only)")
		log.Printf("  GET    /hls/streams              - List streams")
		log.Printf("  GET    /hls/streams/:id          - Get stream details")
		log.Printf("  DELETE /hls/streams/:id          - Delete stream (leader only)")
		log.Printf("  GET    /hls/:id/master.m3u8      - Master playlist")
		log.Printf("")
		log.Printf("Cluster headers:")
		log.Printf("  X-HLS-Node      - Current node ID")
		log.Printf("  X-HLS-Leader    - Leader node ID")
		log.Printf("  X-HLS-Is-Leader - Whether this node is leader")
		log.Printf("")
		log.Printf("Node: %s", *nodeID)

		if err := app.Run(); err != nil {
			log.Printf("Server error: %v", err)
		}
	}()

	// Demonstrate cluster operations
	go demonstrateCluster(app, *nodeID)

	// Wait for interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down...")
	if err := app.Stop(ctx); err != nil {
		log.Printf("Error stopping app: %v", err)
	}
}

func demonstrateCluster(app forge.App, nodeID string) {
	// Wait for startup
	time.Sleep(5 * time.Second)

	ctx := context.Background()

	// Get consensus service
	consensusSvc, err := forge.Resolve[consensus.ConsensusService](app.Container(), "consensus:service")
	if err != nil {
		log.Printf("Failed to resolve consensus service: %v", err)
		return
	}

	// Get HLS service
	hlsSvc, err := forge.Resolve[hls.HLS](app.Container(), "hls")
	if err != nil {
		log.Printf("Failed to resolve HLS service: %v", err)
		return
	}

	// Monitor cluster status
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Get cluster info
			info := consensusSvc.GetClusterInfo()
			isLeader := consensusSvc.IsLeader()
			leader := consensusSvc.GetLeader()

			log.Printf("Cluster Status:")
			log.Printf("  Node: %s", nodeID)
			log.Printf("  Leader: %s", leader)
			log.Printf("  Is Leader: %v", isLeader)
			log.Printf("  Cluster Size: %d", info.TotalNodes)
			log.Printf("  Has Quorum: %v", info.HasQuorum)
			log.Printf("  Healthy Nodes: %d/%d", info.ActiveNodes, info.TotalNodes)

			// List streams
			streams, err := hlsSvc.ListStreams(ctx)
			if err != nil {
				log.Printf("  Failed to list streams: %v", err)
			} else {
				log.Printf("  Active Streams: %d", len(streams))
			}

			log.Printf("")

			// If leader, create a demo stream periodically (every minute)
			if isLeader && time.Now().Second() < 10 {
				demoStream, err := hlsSvc.CreateStream(ctx, hls.StreamOptions{
					Title:          fmt.Sprintf("Demo Stream %s", time.Now().Format("15:04:05")),
					Description:    "Auto-created demo stream",
					Type:           hls.StreamTypeLive,
					TargetDuration: 6,
					DVRWindowSize:  10,
				})
				if err != nil {
					log.Printf("  Failed to create demo stream: %v", err)
				} else {
					log.Printf("  Created demo stream: %s", demoStream.ID)
				}
			}
		}
	}
}
