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

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus"
)

var (
	nodeID    = flag.String("node", "node-1", "Node ID")
	clusterID = flag.String("cluster", "my-cluster", "Cluster ID")
	bindAddr  = flag.String("addr", "0.0.0.0", "Bind address")
	bindPort  = flag.Int("port", 7000, "Bind port")
	httpPort  = flag.Int("http", 8080, "HTTP port")
	peers     = flag.String("peers", "", "Comma-separated peer list (id:addr:port)")
)

func main() {
	flag.Parse()

	// Create Forge app
	app := forge.NewApp(forge.AppConfig{
		Name:    "consensus-example",
		Version: "1.0.0",
	})

	// Parse peers
	peerList := parsePeers(*peers)
	if len(peerList) == 0 {
		// Default 3-node cluster for local testing
		peerList = []consensus.PeerConfig{
			{ID: "node-1", Address: "localhost", Port: 7000},
			{ID: "node-2", Address: "localhost", Port: 7001},
			{ID: "node-3", Address: "localhost", Port: 7002},
		}
	}

	// Register consensus extension
	if err := app.RegisterExtension(consensus.NewExtension(
		consensus.WithNodeID(*nodeID),
		consensus.WithClusterID(*clusterID),
		consensus.WithBindAddress(*bindAddr, *bindPort),
		consensus.WithPeers(peerList),
		consensus.WithStoragePath(fmt.Sprintf("./data/%s", *nodeID)),
	)); err != nil {
		log.Fatalf("Failed to register consensus extension: %v", err)
	}

	// Start the app
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := app.Start(ctx); err != nil {
		log.Fatalf("Failed to start app: %v", err)
	}

	log.Printf("Node %s started on %s:%d", *nodeID, *bindAddr, *bindPort)

	// Get consensus service
	consensusService, err := forge.Resolve[*consensus.Service](app.Container(), "consensus")
	if err != nil {
		log.Fatalf("Failed to resolve consensus service: %v", err)
	}

	// Set up HTTP routes for demonstration
	router, _ := forge.Resolve[forge.Router](app.Container(), "router")
	setupRoutes(router, consensusService, app)

	// Start HTTP server
	go func() {
		log.Printf("HTTP server listening on :%d", *httpPort)
		if err := app.Run(); err != nil {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	// Monitor leadership changes
	go monitorLeadership(ctx, consensusService)

	// Wait for interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := app.Stop(shutdownCtx); err != nil {
		log.Printf("Error during shutdown: %v", err)
	}

	log.Println("Shutdown complete")
}

func setupRoutes(router forge.Router, service *consensus.Service, app forge.App) {
	// Public endpoints (no leadership required)
	router.GET("/", handleHome)
	router.GET("/health", handleHealth(service))
	router.GET("/status", handleStatus(service))

	// Write endpoints (for now, without leadership middleware)
	router.POST("/write", handleWrite(service))

	// Read endpoint (can be on any node)
	router.GET("/read", handleRead(service))

	// Admin endpoints (for now, without leadership middleware)
	router.POST("/admin/transfer-leadership", handleTransferLeadership(service))
	router.POST("/admin/snapshot", handleSnapshot(service))
}

func handleHome(ctx forge.Context) error {
	html := `
	<!DOCTYPE html>
	<html>
	<head>
		<title>Forge Consensus Example</title>
		<style>
			body { font-family: Arial, sans-serif; margin: 40px; }
			.status { padding: 20px; border: 1px solid #ddd; border-radius: 5px; margin: 20px 0; }
			.leader { background-color: #d4edda; }
			.follower { background-color: #d1ecf1; }
			button { padding: 10px 20px; margin: 5px; cursor: pointer; }
		</style>
	</head>
	<body>
		<h1>Forge Consensus Example</h1>
		<div id="status" class="status">Loading...</div>
		<div>
			<button onclick="write()">Write Data</button>
			<button onclick="read()">Read Data</button>
			<button onclick="snapshot()">Create Snapshot</button>
		</div>
		<script>
			function updateStatus() {
				fetch('/status')
					.then(r => r.json())
					.then(data => {
						const isLeader = data.stats.role === 'leader';
						const statusDiv = document.getElementById('status');
						statusDiv.className = 'status ' + (isLeader ? 'leader' : 'follower');
						statusDiv.innerHTML = '<h2>Node Status</h2>' +
							'<p><strong>Node ID:</strong> ' + data.stats.node_id + '</p>' +
							'<p><strong>Role:</strong> ' + data.stats.role + '</p>' +
							'<p><strong>Term:</strong> ' + data.stats.term + '</p>' +
							'<p><strong>Leader:</strong> ' + data.stats.leader_id + '</p>' +
							'<p><strong>Cluster Size:</strong> ' + data.stats.cluster_size + '</p>' +
							'<p><strong>Healthy Nodes:</strong> ' + data.stats.healthy_nodes + '</p>' +
							'<p><strong>Has Quorum:</strong> ' + data.stats.has_quorum + '</p>';
					});
			}

			function write() {
				fetch('/write', {
					method: 'POST',
					headers: {'Content-Type': 'application/json'},
					body: JSON.stringify({key: 'test', value: 'Hello, Consensus!'})
				})
				.then(r => r.json())
				.then(data => alert('Write result: ' + JSON.stringify(data)))
				.catch(err => alert('Write failed: ' + err));
			}

			function read() {
				fetch('/read')
					.then(r => r.json())
					.then(data => alert('Read result: ' + JSON.stringify(data)))
					.catch(err => alert('Read failed: ' + err));
			}

			function snapshot() {
				fetch('/admin/snapshot', {method: 'POST'})
					.then(r => r.json())
					.then(data => alert('Snapshot: ' + data.message))
					.catch(err => alert('Snapshot failed: ' + err));
			}

			// Update status every 2 seconds
			updateStatus();
			setInterval(updateStatus, 2000);
		</script>
	</body>
	</html>
	`
	ctx.Response().Header().Set("Content-Type", "text/html; charset=utf-8")
	ctx.Response().WriteHeader(200)
	_, err := ctx.Response().Write([]byte(html))
	return err
}

func handleHealth(service *consensus.Service) func(forge.Context) error {
	return func(ctx forge.Context) error {
		healthCtx, cancel := context.WithTimeout(ctx.Request().Context(), 5*time.Second)
		defer cancel()

		err := service.HealthCheck(healthCtx)
		if err != nil {
			return ctx.JSON(503, map[string]interface{}{
				"healthy": false,
				"error":   err.Error(),
			})
		}

		return ctx.JSON(200, map[string]interface{}{
			"healthy": true,
		})
	}
}

func handleStatus(service *consensus.Service) func(forge.Context) error {
	return func(ctx forge.Context) error {
		info := service.GetClusterInfo()
		stats := service.GetStats()

		return ctx.JSON(200, map[string]interface{}{
			"cluster": info,
			"stats":   stats,
		})
	}
}

func handleWrite(service *consensus.Service) func(forge.Context) error {
	return func(ctx forge.Context) error {
		var req map[string]interface{}
		if err := ctx.BindJSON(&req); err != nil {
			return ctx.JSON(400, map[string]string{"error": "invalid request"})
		}

		cmd := consensus.Command{
			Type:    "write",
			Payload: req,
		}

		writeCtx, cancel := context.WithTimeout(ctx.Request().Context(), 5*time.Second)
		defer cancel()

		if err := service.Apply(writeCtx, cmd); err != nil {
			if consensus.IsNotLeaderError(err) {
				return ctx.JSON(503, map[string]interface{}{
					"error":     "not the leader",
					"leader_id": service.GetLeader(),
				})
			}
			return ctx.JSON(500, map[string]string{"error": err.Error()})
		}

		return ctx.JSON(200, map[string]interface{}{
			"success": true,
			"message": "command applied",
		})
	}
}

func handleRead(service *consensus.Service) func(forge.Context) error {
	return func(ctx forge.Context) error {
		readCtx, cancel := context.WithTimeout(ctx.Request().Context(), 5*time.Second)
		defer cancel()

		// For this example, just return some demo data
		result, err := service.Read(readCtx, nil)
		if err != nil {
			return ctx.JSON(500, map[string]string{"error": err.Error()})
		}

		return ctx.JSON(200, map[string]interface{}{
			"data":   result,
			"source": service.GetRole(),
		})
	}
}

func handleTransferLeadership(service *consensus.Service) func(forge.Context) error {
	return func(ctx forge.Context) error {
		var req struct {
			TargetNodeID string `json:"target_node_id"`
		}

		if err := ctx.BindJSON(&req); err != nil {
			return ctx.JSON(400, map[string]string{"error": "invalid request"})
		}

		transferCtx, cancel := context.WithTimeout(ctx.Request().Context(), 30*time.Second)
		defer cancel()

		if err := service.TransferLeadership(transferCtx, req.TargetNodeID); err != nil {
			return ctx.JSON(500, map[string]string{"error": err.Error()})
		}

		return ctx.JSON(200, map[string]string{
			"message": "leadership transfer initiated",
			"target":  req.TargetNodeID,
		})
	}
}

func handleSnapshot(service *consensus.Service) func(forge.Context) error {
	return func(ctx forge.Context) error {
		snapshotCtx, cancel := context.WithTimeout(ctx.Request().Context(), 5*time.Minute)
		defer cancel()

		if err := service.Snapshot(snapshotCtx); err != nil {
			return ctx.JSON(500, map[string]string{"error": err.Error()})
		}

		return ctx.JSON(200, map[string]string{
			"message": "snapshot created",
		})
	}
}

func monitorLeadership(ctx context.Context, service *consensus.Service) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var wasLeader bool
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			isLeader := service.IsLeader()
			if isLeader != wasLeader {
				if isLeader {
					log.Printf("✓ This node is now the LEADER (term: %d)", service.GetTerm())
				} else {
					leaderID := service.GetLeader()
					log.Printf("→ This node is now a FOLLOWER (leader: %s, term: %d)", leaderID, service.GetTerm())
				}
				wasLeader = isLeader
			}
		}
	}
}

func parsePeers(peerString string) []consensus.PeerConfig {
	// Parse peer string: "node-1:localhost:7000,node-2:localhost:7001,node-3:localhost:7002"
	// For simplicity, returning empty for now
	// In a real implementation, you would parse this string
	return []consensus.PeerConfig{}
}
