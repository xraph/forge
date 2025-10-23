package helpers

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// DockerHelper provides Docker operations for the CLI
type DockerHelper struct {
	fsHelper *FSHelper
}

// DockerImage represents a Docker image
type DockerImage struct {
	ID         string    `json:"id"`
	Repository string    `json:"repository"`
	Tag        string    `json:"tag"`
	Created    time.Time `json:"created"`
	Size       string    `json:"size"`
	Digest     string    `json:"digest"`
}

// DockerContainer represents a Docker container
type DockerContainer struct {
	ID      string            `json:"id"`
	Names   []string          `json:"names"`
	Image   string            `json:"image"`
	Command string            `json:"command"`
	Created time.Time         `json:"created"`
	Status  string            `json:"status"`
	Ports   []DockerPort      `json:"ports"`
	Labels  map[string]string `json:"labels"`
	Mounts  []DockerMount     `json:"mounts"`
}

// DockerPort represents a container port mapping
type DockerPort struct {
	IP          string `json:"ip"`
	PrivatePort int    `json:"private_port"`
	PublicPort  int    `json:"public_port"`
	Type        string `json:"type"`
}

// DockerMount represents a container mount
type DockerMount struct {
	Type        string `json:"type"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Mode        string `json:"mode"`
	RW          bool   `json:"rw"`
}

// DockerNetwork represents a Docker network
type DockerNetwork struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Driver     string                 `json:"driver"`
	Scope      string                 `json:"scope"`
	Created    time.Time              `json:"created"`
	Internal   bool                   `json:"internal"`
	Attachable bool                   `json:"attachable"`
	Options    map[string]string      `json:"options"`
	Labels     map[string]string      `json:"labels"`
	IPAM       DockerIPAM             `json:"ipam"`
	Containers map[string]interface{} `json:"containers"`
}

// DockerIPAM represents Docker IPAM configuration
type DockerIPAM struct {
	Driver  string            `json:"driver"`
	Config  []DockerIPAMCfg   `json:"config"`
	Options map[string]string `json:"options"`
}

// DockerIPAMCfg represents IPAM configuration
type DockerIPAMCfg struct {
	Subnet     string            `json:"subnet"`
	IPRange    string            `json:"iprange"`
	Gateway    string            `json:"gateway"`
	AuxAddress map[string]string `json:"auxaddress"`
}

// DockerVolume represents a Docker volume
type DockerVolume struct {
	Name       string            `json:"name"`
	Driver     string            `json:"driver"`
	Mountpoint string            `json:"mountpoint"`
	CreatedAt  time.Time         `json:"created_at"`
	Labels     map[string]string `json:"labels"`
	Options    map[string]string `json:"options"`
	Scope      string            `json:"scope"`
}

// DockerBuildOptions represents build options
type DockerBuildOptions struct {
	Context    string            `json:"context"`
	Dockerfile string            `json:"dockerfile"`
	Tag        string            `json:"tag"`
	BuildArgs  map[string]string `json:"build_args"`
	Labels     map[string]string `json:"labels"`
	Target     string            `json:"target"`
	NoCache    bool              `json:"no_cache"`
	Pull       bool              `json:"pull"`
	Quiet      bool              `json:"quiet"`
	Platform   string            `json:"platform"`
}

// DockerRunOptions represents run options
type DockerRunOptions struct {
	Name        string            `json:"name"`
	Image       string            `json:"image"`
	Command     []string          `json:"command"`
	Detach      bool              `json:"detach"`
	Interactive bool              `json:"interactive"`
	TTY         bool              `json:"tty"`
	Remove      bool              `json:"remove"`
	Ports       map[string]string `json:"ports"`
	Volumes     map[string]string `json:"volumes"`
	Environment map[string]string `json:"environment"`
	WorkingDir  string            `json:"working_dir"`
	User        string            `json:"user"`
	Networks    []string          `json:"networks"`
	Labels      map[string]string `json:"labels"`
	Restart     string            `json:"restart"`
	Memory      string            `json:"memory"`
	CPUs        string            `json:"cpus"`
}

// NewDockerHelper creates a new Docker helper
func NewDockerHelper() *DockerHelper {
	return &DockerHelper{
		fsHelper: NewFSHelper(),
	}
}

// IsDockerInstalled checks if Docker is installed and available
func (d *DockerHelper) IsDockerInstalled() bool {
	_, err := exec.LookPath("docker")
	return err == nil
}

// IsDockerRunning checks if Docker daemon is running
func (d *DockerHelper) IsDockerRunning() bool {
	cmd := exec.Command("docker", "info")
	err := cmd.Run()
	return err == nil
}

// GetVersion returns Docker version information
func (d *DockerHelper) GetVersion() (map[string]interface{}, error) {
	if !d.IsDockerInstalled() {
		return nil, fmt.Errorf("docker is not installed")
	}

	cmd := exec.Command("docker", "version", "--format", "{{json .}}")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get docker version: %w", err)
	}

	var version map[string]interface{}
	if err := json.Unmarshal(output, &version); err != nil {
		return nil, fmt.Errorf("failed to parse docker version: %w", err)
	}

	return version, nil
}

// GetInfo returns Docker system information
func (d *DockerHelper) GetInfo() (map[string]interface{}, error) {
	if !d.IsDockerRunning() {
		return nil, fmt.Errorf("docker is not running")
	}

	cmd := exec.Command("docker", "info", "--format", "{{json .}}")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get docker info: %w", err)
	}

	var info map[string]interface{}
	if err := json.Unmarshal(output, &info); err != nil {
		return nil, fmt.Errorf("failed to parse docker info: %w", err)
	}

	return info, nil
}

// Build builds a Docker image
func (d *DockerHelper) Build(options DockerBuildOptions) error {
	if !d.IsDockerRunning() {
		return fmt.Errorf("docker is not running")
	}

	args := []string{"build"}

	if options.Dockerfile != "" {
		args = append(args, "-f", options.Dockerfile)
	}

	if options.Tag != "" {
		args = append(args, "-t", options.Tag)
	}

	if options.Target != "" {
		args = append(args, "--target", options.Target)
	}

	if options.Platform != "" {
		args = append(args, "--platform", options.Platform)
	}

	if options.NoCache {
		args = append(args, "--no-cache")
	}

	if options.Pull {
		args = append(args, "--pull")
	}

	if options.Quiet {
		args = append(args, "--quiet")
	}

	// Add build args
	for key, value := range options.BuildArgs {
		args = append(args, "--build-arg", fmt.Sprintf("%s=%s", key, value))
	}

	// Add labels
	for key, value := range options.Labels {
		args = append(args, "--label", fmt.Sprintf("%s=%s", key, value))
	}

	// Add context (must be last)
	context := options.Context
	if context == "" {
		context = "."
	}
	args = append(args, context)

	cmd := exec.Command("docker", args...)
	cmd.Stdout = nil // Let output show
	cmd.Stderr = nil

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to build image: %w", err)
	}

	return nil
}

// Run runs a Docker container
func (d *DockerHelper) Run(options DockerRunOptions) (string, error) {
	if !d.IsDockerRunning() {
		return "", fmt.Errorf("docker is not running")
	}

	args := []string{"run"}

	if options.Name != "" {
		args = append(args, "--name", options.Name)
	}

	if options.Detach {
		args = append(args, "-d")
	}

	if options.Interactive {
		args = append(args, "-i")
	}

	if options.TTY {
		args = append(args, "-t")
	}

	if options.Remove {
		args = append(args, "--rm")
	}

	if options.WorkingDir != "" {
		args = append(args, "-w", options.WorkingDir)
	}

	if options.User != "" {
		args = append(args, "-u", options.User)
	}

	if options.Restart != "" {
		args = append(args, "--restart", options.Restart)
	}

	if options.Memory != "" {
		args = append(args, "-m", options.Memory)
	}

	if options.CPUs != "" {
		args = append(args, "--cpus", options.CPUs)
	}

	// Add port mappings
	for hostPort, containerPort := range options.Ports {
		args = append(args, "-p", fmt.Sprintf("%s:%s", hostPort, containerPort))
	}

	// Add volume mounts
	for hostPath, containerPath := range options.Volumes {
		args = append(args, "-v", fmt.Sprintf("%s:%s", hostPath, containerPath))
	}

	// Add environment variables
	for key, value := range options.Environment {
		args = append(args, "-e", fmt.Sprintf("%s=%s", key, value))
	}

	// Add networks
	for _, network := range options.Networks {
		args = append(args, "--network", network)
	}

	// Add labels
	for key, value := range options.Labels {
		args = append(args, "--label", fmt.Sprintf("%s=%s", key, value))
	}

	// Add image
	args = append(args, options.Image)

	// Add command
	args = append(args, options.Command...)

	cmd := exec.Command("docker", args...)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to run container: %w", err)
	}

	containerID := strings.TrimSpace(string(output))
	return containerID, nil
}

// Stop stops a running container
func (d *DockerHelper) Stop(containerID string, timeout int) error {
	args := []string{"stop"}
	if timeout > 0 {
		args = append(args, "-t", strconv.Itoa(timeout))
	}
	args = append(args, containerID)

	cmd := exec.Command("docker", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to stop container %s: %w", containerID, err)
	}

	return nil
}

// Start starts a stopped container
func (d *DockerHelper) Start(containerID string) error {
	cmd := exec.Command("docker", "start", containerID)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to start container %s: %w", containerID, err)
	}

	return nil
}

// Remove removes a container
func (d *DockerHelper) Remove(containerID string, force bool) error {
	args := []string{"rm"}
	if force {
		args = append(args, "-f")
	}
	args = append(args, containerID)

	cmd := exec.Command("docker", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to remove container %s: %w", containerID, err)
	}

	return nil
}

// GetContainers returns a list of containers
func (d *DockerHelper) GetContainers(all bool) ([]*DockerContainer, error) {
	args := []string{"ps", "--format", "{{json .}}"}
	if all {
		args = append(args, "-a")
	}

	cmd := exec.Command("docker", args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get containers: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	containers := make([]*DockerContainer, 0, len(lines))

	for _, line := range lines {
		if line == "" {
			continue
		}

		var container DockerContainer
		if err := json.Unmarshal([]byte(line), &container); err != nil {
			continue // Skip invalid JSON
		}

		containers = append(containers, &container)
	}

	return containers, nil
}

// GetContainer returns detailed information about a specific container
func (d *DockerHelper) GetContainer(containerID string) (*DockerContainer, error) {
	cmd := exec.Command("docker", "inspect", containerID)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container %s: %w", containerID, err)
	}

	var containers []DockerContainer
	if err := json.Unmarshal(output, &containers); err != nil {
		return nil, fmt.Errorf("failed to parse container info: %w", err)
	}

	if len(containers) == 0 {
		return nil, fmt.Errorf("container %s not found", containerID)
	}

	return &containers[0], nil
}

// GetImages returns a list of Docker images
func (d *DockerHelper) GetImages() ([]*DockerImage, error) {
	cmd := exec.Command("docker", "images", "--format", "{{json .}}")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get images: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	images := make([]*DockerImage, 0, len(lines))

	for _, line := range lines {
		if line == "" {
			continue
		}

		var image DockerImage
		if err := json.Unmarshal([]byte(line), &image); err != nil {
			continue // Skip invalid JSON
		}

		images = append(images, &image)
	}

	return images, nil
}

// RemoveImage removes a Docker image
func (d *DockerHelper) RemoveImage(imageID string, force bool) error {
	args := []string{"rmi"}
	if force {
		args = append(args, "-f")
	}
	args = append(args, imageID)

	cmd := exec.Command("docker", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to remove image %s: %w", imageID, err)
	}

	return nil
}

// Pull pulls a Docker image
func (d *DockerHelper) Pull(image string, platform string) error {
	args := []string{"pull"}
	if platform != "" {
		args = append(args, "--platform", platform)
	}
	args = append(args, image)

	cmd := exec.Command("docker", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to pull image %s: %w", image, err)
	}

	return nil
}

// Push pushes a Docker image
func (d *DockerHelper) Push(image string) error {
	cmd := exec.Command("docker", "push", image)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to push image %s: %w", image, err)
	}

	return nil
}

// Tag tags a Docker image
func (d *DockerHelper) Tag(sourceImage, targetImage string) error {
	cmd := exec.Command("docker", "tag", sourceImage, targetImage)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to tag image %s as %s: %w", sourceImage, targetImage, err)
	}

	return nil
}

// GetLogs returns container logs
func (d *DockerHelper) GetLogs(containerID string, follow bool, tail int) ([]byte, error) {
	args := []string{"logs"}
	if follow {
		args = append(args, "-f")
	}
	if tail > 0 {
		args = append(args, "--tail", strconv.Itoa(tail))
	}
	args = append(args, containerID)

	cmd := exec.Command("docker", args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get logs for container %s: %w", containerID, err)
	}

	return output, nil
}

// Execute executes a command in a running container
func (d *DockerHelper) Execute(containerID string, command []string, interactive bool) error {
	args := []string{"exec"}
	if interactive {
		args = append(args, "-it")
	}
	args = append(args, containerID)
	args = append(args, command...)

	cmd := exec.Command("docker", args...)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to execute command in container %s: %w", containerID, err)
	}

	return nil
}

// CreateNetwork creates a Docker network
func (d *DockerHelper) CreateNetwork(name, driver string, options map[string]string) error {
	args := []string{"network", "create"}
	if driver != "" {
		args = append(args, "--driver", driver)
	}

	for key, value := range options {
		args = append(args, "--opt", fmt.Sprintf("%s=%s", key, value))
	}

	args = append(args, name)

	cmd := exec.Command("docker", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create network %s: %w", name, err)
	}

	return nil
}

// RemoveNetwork removes a Docker network
func (d *DockerHelper) RemoveNetwork(name string) error {
	cmd := exec.Command("docker", "network", "rm", name)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to remove network %s: %w", name, err)
	}

	return nil
}

// GetNetworks returns a list of Docker networks
func (d *DockerHelper) GetNetworks() ([]*DockerNetwork, error) {
	cmd := exec.Command("docker", "network", "ls", "--format", "{{json .}}")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get networks: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	networks := make([]*DockerNetwork, 0, len(lines))

	for _, line := range lines {
		if line == "" {
			continue
		}

		var network DockerNetwork
		if err := json.Unmarshal([]byte(line), &network); err != nil {
			continue // Skip invalid JSON
		}

		networks = append(networks, &network)
	}

	return networks, nil
}

// CreateVolume creates a Docker volume
func (d *DockerHelper) CreateVolume(name, driver string, options map[string]string, labels map[string]string) error {
	args := []string{"volume", "create"}
	if driver != "" {
		args = append(args, "--driver", driver)
	}

	for key, value := range options {
		args = append(args, "--opt", fmt.Sprintf("%s=%s", key, value))
	}

	for key, value := range labels {
		args = append(args, "--label", fmt.Sprintf("%s=%s", key, value))
	}

	args = append(args, name)

	cmd := exec.Command("docker", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create volume %s: %w", name, err)
	}

	return nil
}

// RemoveVolume removes a Docker volume
func (d *DockerHelper) RemoveVolume(name string, force bool) error {
	args := []string{"volume", "rm"}
	if force {
		args = append(args, "-f")
	}
	args = append(args, name)

	cmd := exec.Command("docker", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to remove volume %s: %w", name, err)
	}

	return nil
}

// GetVolumes returns a list of Docker volumes
func (d *DockerHelper) GetVolumes() ([]*DockerVolume, error) {
	cmd := exec.Command("docker", "volume", "ls", "--format", "{{json .}}")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get volumes: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	volumes := make([]*DockerVolume, 0, len(lines))

	for _, line := range lines {
		if line == "" {
			continue
		}

		var volume DockerVolume
		if err := json.Unmarshal([]byte(line), &volume); err != nil {
			continue // Skip invalid JSON
		}

		volumes = append(volumes, &volume)
	}

	return volumes, nil
}

// Prune removes unused Docker objects
func (d *DockerHelper) Prune(pruneType string, force bool) error {
	args := []string{pruneType, "prune"}
	if force {
		args = append(args, "-f")
	}

	cmd := exec.Command("docker", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to prune %s: %w", pruneType, err)
	}

	return nil
}

// SystemPrune removes all unused Docker objects
func (d *DockerHelper) SystemPrune(force, volumes bool) error {
	args := []string{"system", "prune"}
	if force {
		args = append(args, "-f")
	}
	if volumes {
		args = append(args, "--volumes")
	}

	cmd := exec.Command("docker", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to prune system: %w", err)
	}

	return nil
}

// GetStats returns container statistics
func (d *DockerHelper) GetStats(containerID string) (map[string]interface{}, error) {
	cmd := exec.Command("docker", "stats", "--no-stream", "--format", "{{json .}}", containerID)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get stats for container %s: %w", containerID, err)
	}

	var stats map[string]interface{}
	if err := json.Unmarshal(output, &stats); err != nil {
		return nil, fmt.Errorf("failed to parse stats: %w", err)
	}

	return stats, nil
}

// WaitForContainer waits for a container to reach a certain state
func (d *DockerHelper) WaitForContainer(containerID string, condition string) error {
	args := []string{"wait"}
	if condition != "" {
		args = append(args, "--condition", condition)
	}
	args = append(args, containerID)

	cmd := exec.Command("docker", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to wait for container %s: %w", containerID, err)
	}

	return nil
}

// CopyFromContainer copies files from a container to the host
func (d *DockerHelper) CopyFromContainer(containerID, srcPath, destPath string) error {
	cmd := exec.Command("docker", "cp", fmt.Sprintf("%s:%s", containerID, srcPath), destPath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to copy from container %s: %w", containerID, err)
	}

	return nil
}

// CopyToContainer copies files from the host to a container
func (d *DockerHelper) CopyToContainer(srcPath, containerID, destPath string) error {
	cmd := exec.Command("docker", "cp", srcPath, fmt.Sprintf("%s:%s", containerID, destPath))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to copy to container %s: %w", containerID, err)
	}

	return nil
}
