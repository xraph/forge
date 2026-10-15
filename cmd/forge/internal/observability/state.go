// Package observability runs forge's local Grafana stack and renders the
// dashboard and alert rules that ship with the CLI.
package observability

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

//go:embed assets
var assets embed.FS

const (
	// Image is the all-in-one Grafana, Tempo, Loki, Prometheus and collector image.
	Image = "grafana/otel-lgtm:0.36.0"
	// ContainerName is the one long-lived stack container.
	ContainerName = "forge-lgtm"
	// VolumeName holds the stack's data across recreates.
	VolumeName = "forge-lgtm-data"
	// NetworkName is the user network the stack runs on. Dev containers
	// join it so they reach the stack, and it them, by container name.
	NetworkName = "forge-observability"

	labelHash  = "forge.config-hash"
	labelPorts = "forge.ports"
)

// Ports are the host ports the stack publishes on 127.0.0.1.
type Ports struct {
	Grafana    int
	OTLP       int
	Prometheus int
}

// DefaultPorts are the image's own ports.
func DefaultPorts() Ports {
	return Ports{Grafana: 3000, OTLP: 4318, Prometheus: 9090}
}

func (p Ports) String() string {
	return fmt.Sprintf("%d,%d,%d", p.Grafana, p.OTLP, p.Prometheus)
}

// GrafanaURL is where Grafana answers on the host.
func (p Ports) GrafanaURL() string { return "http://localhost:" + strconv.Itoa(p.Grafana) }

// OTLPURL is the OTLP/HTTP endpoint on the host.
func (p Ports) OTLPURL() string { return "http://localhost:" + strconv.Itoa(p.OTLP) }

// PrometheusURL is where Prometheus answers on the host.
func (p Ports) PrometheusURL() string { return "http://localhost:" + strconv.Itoa(p.Prometheus) }

// ParsePorts reads the forge.ports container label.
func ParsePorts(s string) (Ports, error) {
	parts := strings.Split(s, ",")
	if len(parts) != 3 {
		return Ports{}, fmt.Errorf("observability: bad ports label %q", s)
	}

	nums := make([]int, 3)

	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 1 || n > 65535 {
			return Ports{}, fmt.Errorf("observability: bad ports label %q", s)
		}

		nums[i] = n
	}

	return Ports{Grafana: nums[0], OTLP: nums[1], Prometheus: nums[2]}, nil
}

// DefaultStateDir is where the CLI keeps the files it mounts into the stack.
func DefaultStateDir() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", errors.Join(errors.New("observability: no user cache directory"), err)
	}

	return filepath.Join(cache, "forge", "observability"), nil
}

// WriteState writes the generated files into dir and returns a hash of their
// contents. Target files are left alone and are not part of the hash, because
// Prometheus reads them live.
func WriteState(dir string) (string, error) {
	dashboard, err := DashboardJSON()
	if err != nil {
		return "", err
	}

	files := []struct {
		path string
		data []byte
	}{{path: "dashboards/forge-service.json", data: dashboard}}

	for _, name := range []string{"prometheus.yaml", "grafana-datasources.yaml", "grafana-dashboards-forge.yaml"} {
		raw, err := assets.ReadFile("assets/" + name)
		if err != nil {
			return "", fmt.Errorf("observability: read %s: %w", name, err)
		}

		files = append(files, struct {
			path string
			data []byte
		}{path: name, data: raw})
	}

	h := sha256.New()

	for _, f := range files {
		full := filepath.Join(dir, filepath.FromSlash(f.path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return "", fmt.Errorf("observability: create %s: %w", filepath.Dir(full), err)
		}

		// os.WriteFile truncates in place, which keeps the inode a single-file
		// bind mount points at.
		if err := os.WriteFile(full, f.data, 0o644); err != nil {
			return "", fmt.Errorf("observability: write %s: %w", full, err)
		}

		h.Write([]byte(f.path))
		h.Write([]byte{0})
		h.Write(f.data)
		h.Write([]byte{0})
	}

	if err := os.MkdirAll(filepath.Join(dir, "targets"), 0o755); err != nil {
		return "", fmt.Errorf("observability: create targets dir: %w", err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
