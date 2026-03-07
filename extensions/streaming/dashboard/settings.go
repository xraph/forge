package dashboard

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/a-h/templ"

	"github.com/xraph/forge/extensions/streaming/internal"
)

// configSettings renders the streaming configuration settings panel.
func configSettings(config internal.Config) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		html := `<div class="space-y-6">`

		// Backend section
		html += settingsSection("Backend", []settingsRow{
			{"Backend Type", config.Backend},
			{"Backend URLs", formatStringSlice(config.BackendURLs)},
			{"Distributed Mode", formatBool(config.EnableDistributed)},
			{"Node ID", formatStringDefault(config.NodeID, "auto-generated")},
		})

		// Features section
		html += settingsSection("Features", []settingsRow{
			{"Rooms", formatBool(config.EnableRooms)},
			{"Channels", formatBool(config.EnableChannels)},
			{"Presence", formatBool(config.EnablePresence)},
			{"Typing Indicators", formatBool(config.EnableTypingIndicators)},
			{"Message History", formatBool(config.EnableMessageHistory)},
			{"Session Resumption", formatBool(config.EnableSessionResumption)},
			{"Load Balancer", formatBool(config.EnableLoadBalancer)},
		})

		// Limits section
		html += settingsSection("Limits", []settingsRow{
			{"Max Connections/User", fmt.Sprintf("%d", config.MaxConnectionsPerUser)},
			{"Max Rooms/User", fmt.Sprintf("%d", config.MaxRoomsPerUser)},
			{"Max Channels/User", fmt.Sprintf("%d", config.MaxChannelsPerUser)},
			{"Max Message Size", formatBytes(config.MaxMessageSize)},
			{"Max Messages/Second", fmt.Sprintf("%d", config.MaxMessagesPerSecond)},
			{"Max Messages/Room", fmt.Sprintf("%d", config.MaxMessagesPerRoom)},
		})

		// Timeouts section
		html += settingsSection("Timeouts", []settingsRow{
			{"Ping Interval", config.PingInterval.String()},
			{"Pong Timeout", config.PongTimeout.String()},
			{"Write Timeout", config.WriteTimeout.String()},
			{"Presence Timeout", config.PresenceTimeout.String()},
			{"Typing Timeout", config.TypingTimeout.String()},
			{"Message Retention", config.MessageRetention.String()},
		})

		// Buffers section
		html += settingsSection("Buffers", []settingsRow{
			{"Read Buffer Size", formatBytes(config.ReadBufferSize)},
			{"Write Buffer Size", formatBytes(config.WriteBufferSize)},
		})

		// TLS section
		if config.TLSEnabled {
			html += settingsSection("TLS", []settingsRow{
				{"Enabled", "Yes"},
				{"Cert File", config.TLSCertFile},
				{"Key File", config.TLSKeyFile},
				{"CA File", formatStringDefault(config.TLSCAFile, "not set")},
			})
		}

		// Load Balancer section (if enabled)
		if config.EnableLoadBalancer {
			html += settingsSection("Load Balancer", []settingsRow{
				{"Strategy", config.LoadBalancerStrategy},
				{"Health Check Interval", config.HealthCheckInterval.String()},
				{"Health Check Timeout", config.HealthCheckTimeout.String()},
				{"Sticky Session TTL", config.StickySessionTTL.String()},
				{"Consistent Hash Replicas", fmt.Sprintf("%d", config.ConsistentHashReplicas)},
			})
		}

		html += `</div>`

		_, err := io.WriteString(w, html)
		return err
	})
}

type settingsRow struct {
	Label string
	Value string
}

func settingsSection(title string, rows []settingsRow) string {
	html := `<div>` +
		`<h4 class="text-sm font-semibold mb-3 text-muted-foreground uppercase tracking-wider">` + templ.EscapeString(title) + `</h4>` +
		`<div class="rounded-md border divide-y">`

	for _, row := range rows {
		html += `<div class="flex items-center justify-between px-4 py-2.5">` +
			`<span class="text-sm">` + templ.EscapeString(row.Label) + `</span>` +
			`<span class="text-sm font-mono text-muted-foreground">` + templ.EscapeString(row.Value) + `</span>` +
			`</div>`
	}

	html += `</div></div>`
	return html
}

func formatBool(b bool) string {
	if b {
		return "enabled"
	}

	return "disabled"
}

func formatBytes(b int) string {
	if b < 1024 {
		return fmt.Sprintf("%d B", b)
	}

	if b < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	}

	return fmt.Sprintf("%.1f MB", float64(b)/(1024*1024))
}

func formatStringSlice(s []string) string {
	if len(s) == 0 {
		return "none"
	}

	return strings.Join(s, ", ")
}

func formatStringDefault(s, def string) string {
	if s == "" {
		return def
	}

	return s
}
