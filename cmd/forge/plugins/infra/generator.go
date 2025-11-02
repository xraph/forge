// v2/cmd/forge/plugins/infra/generator.go
package infra

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xraph/forge/cmd/forge/config"
)

// Generator handles infrastructure code generation
type Generator struct {
	config      *config.ForgeConfig
	Introspect  *Introspector
}

// NewGenerator creates a new infrastructure generator
func NewGenerator(cfg *config.ForgeConfig) *Generator {
	return &Generator{
		config:      cfg,
		Introspect:  NewIntrospector(cfg),
	}
}

// GeneratedConfig represents the result of infrastructure generation
type GeneratedConfig struct {
	ServiceCount    int
	NetworkCount    int
	DeploymentCount int
	VolumeCount     int
}

// ========================================
// Docker Generation
// ========================================

// GenerateDockerCompose generates Docker Compose configuration in-memory
func (g *Generator) GenerateDockerCompose(service, env string) (*GeneratedConfig, error) {
	apps, err := g.Introspect.DiscoverApps()
	if err != nil {
		return nil, fmt.Errorf("failed to discover apps: %w", err)
	}

	if service != "" {
		// Filter for specific service
		apps = filterApps(apps, service)
	}

	config := &GeneratedConfig{
		ServiceCount: len(apps),
		NetworkCount: 1, // Default network
	}

	return config, nil
}

// ExportDocker exports Docker configuration to filesystem
func (g *Generator) ExportDocker(outputDir string) error {
	apps, err := g.Introspect.DiscoverApps()
	if err != nil {
		return fmt.Errorf("failed to discover apps: %w", err)
	}

	// Create output directory
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Generate docker-compose.yml
	composeContent := g.generateDockerComposeYAML(apps, "base")
	if err := os.WriteFile(filepath.Join(outputDir, "docker-compose.yml"), []byte(composeContent), 0644); err != nil {
		return fmt.Errorf("failed to write docker-compose.yml: %w", err)
	}

	// Generate environment-specific overrides
	for _, env := range []string{"dev", "prod"} {
		composeEnvContent := g.generateDockerComposeYAML(apps, env)
		filename := fmt.Sprintf("docker-compose.%s.yml", env)
		if err := os.WriteFile(filepath.Join(outputDir, filename), []byte(composeEnvContent), 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", filename, err)
		}
	}

	// Generate .env.example
	envContent := g.generateEnvExample(apps)
	if err := os.WriteFile(filepath.Join(outputDir, ".env.example"), []byte(envContent), 0644); err != nil {
		return fmt.Errorf("failed to write .env.example: %w", err)
	}

	// Generate Dockerfiles for each service at root level
	for _, app := range apps {
		dockerfileContent := g.generateDockerfile(app)
		dockerfileName := fmt.Sprintf("Dockerfile.%s", app.Name)
		if err := os.WriteFile(filepath.Join(outputDir, dockerfileName), []byte(dockerfileContent), 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", dockerfileName, err)
		}
	}

	return nil
}

func (g *Generator) generateDockerComposeYAML(apps []AppInfo, environment string) string {
	var content string

	if environment == "base" {
		content = `version: '3.8'

services:
`
	} else {
		content = fmt.Sprintf(`version: '3.8'

# Environment-specific overrides for %s
services:
`, environment)
	}

	for _, app := range apps {
		if environment == "base" {
			content += fmt.Sprintf(`  %s:
    build:
      context: .
      dockerfile: Dockerfile.%s
    ports:
      - "${%s_PORT:-8080}:8080"
    environment:
      - ENV=%s
      - LOG_LEVEL=${LOG_LEVEL:-info}
    networks:
      - forge-network
    restart: unless-stopped

`, app.Name, app.Name, normalizeEnvVar(app.Name), environment)
		} else {
			// Environment-specific overrides
			replicas := "1"
			if environment == "prod" {
				replicas = "3"
			}
			
			content += fmt.Sprintf(`  %s:
    deploy:
      replicas: %s
      resources:
        limits:
          cpus: '1'
          memory: 512M
        reservations:
          cpus: '0.5'
          memory: 256M

`, app.Name, replicas)
		}
	}

	if environment == "base" {
		content += `networks:
  forge-network:
    driver: bridge
`
	}

	return content
}

func (g *Generator) generateDockerfile(app AppInfo) string {
	modulePath := g.config.Project.Module
	if modulePath == "" {
		modulePath = "github.com/example/project"
	}

	return fmt.Sprintf(`# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main %s

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy the binary from builder
COPY --from=builder /app/main .

# Expose port
EXPOSE 8080

# Run the binary
CMD ["./main"]
`, app.MainPath)
}

func (g *Generator) generateEnvExample(apps []AppInfo) string {
	content := `# Docker Compose Environment Variables
# Copy this file to .env and update with your values

# Application Environment
ENV=dev
LOG_LEVEL=info

# Service Ports
`
	for _, app := range apps {
		content += fmt.Sprintf("%s_PORT=8080\n", normalizeEnvVar(app.Name))
	}

	content += `
# Database (if needed)
DATABASE_URL=postgres://user:password@postgres:5432/dbname

# Redis (if needed)
REDIS_URL=redis://redis:6379

# API Keys
# API_KEY=your-api-key-here
`
	return content
}

// ========================================
// Kubernetes Generation
// ========================================

// GenerateK8sManifests generates Kubernetes manifests in-memory
func (g *Generator) GenerateK8sManifests(service, env string) (*GeneratedConfig, error) {
	apps, err := g.Introspect.DiscoverApps()
	if err != nil {
		return nil, fmt.Errorf("failed to discover apps: %w", err)
	}

	if service != "" {
		apps = filterApps(apps, service)
	}

	config := &GeneratedConfig{
		ServiceCount:    len(apps),
		DeploymentCount: len(apps),
	}

	return config, nil
}

// ExportK8s exports Kubernetes manifests to filesystem
func (g *Generator) ExportK8s(outputDir string) error {
	apps, err := g.Introspect.DiscoverApps()
	if err != nil {
		return fmt.Errorf("failed to discover apps: %w", err)
	}

	// Create base directory
	baseDir := filepath.Join(outputDir, "base")
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return fmt.Errorf("failed to create base directory: %w", err)
	}

	// Generate base deployment manifests
	deploymentContent := g.generateK8sDeployment(apps)
	if err := os.WriteFile(filepath.Join(baseDir, "deployment.yaml"), []byte(deploymentContent), 0644); err != nil {
		return fmt.Errorf("failed to write deployment.yaml: %w", err)
	}

	// Generate base service manifests
	serviceContent := g.generateK8sService(apps)
	if err := os.WriteFile(filepath.Join(baseDir, "service.yaml"), []byte(serviceContent), 0644); err != nil {
		return fmt.Errorf("failed to write service.yaml: %w", err)
	}

	// Generate base kustomization
	kustomizeContent := g.generateK8sKustomization(apps, "base")
	if err := os.WriteFile(filepath.Join(baseDir, "kustomization.yaml"), []byte(kustomizeContent), 0644); err != nil {
		return fmt.Errorf("failed to write kustomization.yaml: %w", err)
	}

	// Generate overlays for different environments
	for _, env := range []string{"dev", "prod"} {
		overlayDir := filepath.Join(outputDir, "overlays", env)
		if err := os.MkdirAll(overlayDir, 0755); err != nil {
			return fmt.Errorf("failed to create overlay directory: %w", err)
		}

		kustomizeOverlay := g.generateK8sKustomizationOverlay(apps, env)
		if err := os.WriteFile(filepath.Join(overlayDir, "kustomization.yaml"), []byte(kustomizeOverlay), 0644); err != nil {
			return fmt.Errorf("failed to write overlay kustomization.yaml: %w", err)
		}
	}

	return nil
}

func (g *Generator) generateK8sDeployment(apps []AppInfo) string {
	content := ""

	for i, app := range apps {
		if i > 0 {
			content += "---\n"
		}

		registryImage := "registry.example.com"
		if g.config.Deploy.Registry != "" {
			registryImage = g.config.Deploy.Registry
		}

		content += fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  labels:
    app: %s
spec:
  replicas: 1
  selector:
    matchLabels:
      app: %s
  template:
    metadata:
      labels:
        app: %s
    spec:
      containers:
      - name: %s
        image: %s/%s:latest
        ports:
        - containerPort: 8080
          name: http
        env:
        - name: ENV
          value: "dev"
        - name: LOG_LEVEL
          value: "info"
        resources:
          requests:
            memory: "256Mi"
            cpu: "250m"
          limits:
            memory: "512Mi"
            cpu: "500m"
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /ready
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 5
`, app.Name, app.Name, app.Name, app.Name, app.Name, registryImage, app.Name)
	}

	return content
}

func (g *Generator) generateK8sService(apps []AppInfo) string {
	content := ""

	for i, app := range apps {
		if i > 0 {
			content += "---\n"
		}

		content += fmt.Sprintf(`apiVersion: v1
kind: Service
metadata:
  name: %s
  labels:
    app: %s
spec:
  type: ClusterIP
  ports:
  - port: 80
    targetPort: 8080
    protocol: TCP
    name: http
  selector:
    app: %s
`, app.Name, app.Name, app.Name)
	}

	return content
}

func (g *Generator) generateK8sKustomization(apps []AppInfo, env string) string {
	content := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
  - deployment.yaml
  - service.yaml

commonLabels:
  environment: ` + env + `
`
	return content
}

func (g *Generator) generateK8sKustomizationOverlay(apps []AppInfo, env string) string {
	replicas := 1
	if env == "prod" {
		replicas = 3
	}

	content := fmt.Sprintf(`apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

bases:
  - ../../base

namePrefix: %s-

replicas:
`, env)

	for _, app := range apps {
		content += fmt.Sprintf(`  - name: %s
    count: %d
`, app.Name, replicas)
	}

	content += `
commonLabels:
  environment: ` + env + `
`

	return content
}

// ========================================
// Digital Ocean Generation
// ========================================

// GenerateDOConfig generates Digital Ocean App Platform configuration in-memory
func (g *Generator) GenerateDOConfig(service, env, region string) (*GeneratedConfig, error) {
	apps, err := g.Introspect.DiscoverApps()
	if err != nil {
		return nil, fmt.Errorf("failed to discover apps: %w", err)
	}

	if service != "" {
		apps = filterApps(apps, service)
	}

	config := &GeneratedConfig{
		ServiceCount: len(apps),
	}

	return config, nil
}

// ExportDO exports Digital Ocean configuration to filesystem
func (g *Generator) ExportDO(outputDir string) error {
	apps, err := g.Introspect.DiscoverApps()
	if err != nil {
		return fmt.Errorf("failed to discover apps: %w", err)
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Generate app.yaml
	appSpecContent := g.generateDOAppSpec(apps)
	if err := os.WriteFile(filepath.Join(outputDir, "app.yaml"), []byte(appSpecContent), 0644); err != nil {
		return fmt.Errorf("failed to write app.yaml: %w", err)
	}

	// Generate .env.example
	envContent := g.generateEnvExample(apps)
	if err := os.WriteFile(filepath.Join(outputDir, ".env.example"), []byte(envContent), 0644); err != nil {
		return fmt.Errorf("failed to write .env.example: %w", err)
	}

	// Generate README
	readmeContent := g.generateDOReadme()
	if err := os.WriteFile(filepath.Join(outputDir, "README.md"), []byte(readmeContent), 0644); err != nil {
		return fmt.Errorf("failed to write README.md: %w", err)
	}

	return nil
}

func (g *Generator) generateDOAppSpec(apps []AppInfo) string {
	// Extract git repository info
	gitRepo := g.extractGitRepo()
	gitBranch := g.getGitBranch()
	deployOnPush := g.getDeployOnPush()
	
	// Get region from config or use default
	region := "nyc1"
	if g.config.Deploy.DigitalOcean.Region != "" {
		region = g.config.Deploy.DigitalOcean.Region
	}
	
	content := fmt.Sprintf(`name: %s
region: %s

services:
`, g.config.Project.Name, region)

	for _, app := range apps {
		content += fmt.Sprintf(`  - name: %s
    github:
      repo: %s
      branch: %s
      deploy_on_push: %t
    dockerfile_path: Dockerfile.%s
    http_port: 8080
    instance_count: 1
    instance_size_slug: basic-xxs
    routes:
      - path: /
    envs:
      - key: ENV
        value: production
      - key: LOG_LEVEL
        value: info
    health_check:
      http_path: /health
      initial_delay_seconds: 30
      period_seconds: 10

`, app.Name, gitRepo, gitBranch, deployOnPush, app.Name)
	}

	return content
}

func (g *Generator) generateDOReadme() string {
	gitRepo := g.extractGitRepo()
	return `# Digital Ocean Deployment

## Prerequisites

- Digital Ocean account
- doctl CLI tool installed
- GitHub repository connected to Digital Ocean

## Repository Configuration

The app.yaml has been configured with:
- Repository: ` + gitRepo + `
- Branch: ` + g.getGitBranch() + `
- Auto-deploy on push: ` + fmt.Sprintf("%t", g.getDeployOnPush()) + `

Repository information was automatically extracted from go.mod. To customize:

` + "```yaml" + `
# .forge.yaml
deploy:
  digitalocean:
    git_repo: your-org/your-repo
    git_branch: main
    deploy_on_push: true
    region: nyc1
` + "```" + `

## Deployment

1. Review the app.yaml configuration
2. Deploy using doctl:
   ` + "`" + `bash
   doctl apps create --spec app.yaml
   ` + "`" + `

3. Or use the Forge CLI:
   ` + "`" + `bash
   forge infra do deploy
   ` + "`" + `

## Configuration

Update the environment variables in the Digital Ocean dashboard or modify app.yaml.

## Monitoring

Access logs and metrics in the Digital Ocean dashboard:
https://cloud.digitalocean.com/apps
`
}

// ========================================
// Render Generation
// ========================================

// GenerateRenderConfig generates Render.com configuration in-memory
func (g *Generator) GenerateRenderConfig(service, env string) (*GeneratedConfig, error) {
	apps, err := g.Introspect.DiscoverApps()
	if err != nil {
		return nil, fmt.Errorf("failed to discover apps: %w", err)
	}

	if service != "" {
		apps = filterApps(apps, service)
	}

	config := &GeneratedConfig{
		ServiceCount: len(apps),
	}

	return config, nil
}

// ExportRender exports Render configuration to filesystem
func (g *Generator) ExportRender(outputDir string) error {
	apps, err := g.Introspect.DiscoverApps()
	if err != nil {
		return fmt.Errorf("failed to discover apps: %w", err)
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Generate render.yaml
	renderSpecContent := g.generateRenderSpec(apps)
	if err := os.WriteFile(filepath.Join(outputDir, "render.yaml"), []byte(renderSpecContent), 0644); err != nil {
		return fmt.Errorf("failed to write render.yaml: %w", err)
	}

	// Generate .env.example
	envContent := g.generateEnvExample(apps)
	if err := os.WriteFile(filepath.Join(outputDir, ".env.example"), []byte(envContent), 0644); err != nil {
		return fmt.Errorf("failed to write .env.example: %w", err)
	}

	// Generate README
	readmeContent := g.generateRenderReadme()
	if err := os.WriteFile(filepath.Join(outputDir, "README.md"), []byte(readmeContent), 0644); err != nil {
		return fmt.Errorf("failed to write README.md: %w", err)
	}

	return nil
}

func (g *Generator) generateRenderSpec(apps []AppInfo) string {
	// Extract git repository info
	gitRepo := g.extractGitRepo()
	gitBranch := g.getGitBranch()
	
	content := `services:
`

	for _, app := range apps {
		content += fmt.Sprintf(`  - type: web
    name: %s
    env: go
    repo: https://github.com/%s
    branch: %s
    buildCommand: go build -o bin/main %s
    startCommand: ./bin/main
    envVars:
      - key: ENV
        value: production
      - key: LOG_LEVEL
        value: info
      - key: PORT
        value: 8080
    healthCheckPath: /health
    numInstances: 1

`, app.Name, gitRepo, gitBranch, app.MainPath)
	}

	return content
}

func (g *Generator) generateRenderReadme() string {
	gitRepo := g.extractGitRepo()
	return `# Render.com Deployment

## Prerequisites

- Render.com account
- GitHub repository connected to Render

## Repository Configuration

The render.yaml has been configured with:
- Repository: https://github.com/` + gitRepo + `
- Branch: ` + g.getGitBranch() + `

Repository information was automatically extracted from go.mod. To customize:

` + "```yaml" + `
# .forge.yaml
deploy:
  render:
    git_repo: your-org/your-repo
    git_branch: main
    region: oregon
` + "```" + `

## Deployment

1. Connect your GitHub repository to Render
2. Use the render.yaml blueprint for automatic setup:
   - Go to https://dashboard.render.com
   - Click "New" -> "Blueprint"
   - Select your repository
   - Render will automatically detect render.yaml

3. Or deploy via Forge CLI:
   ` + "`" + `bash
   forge infra render deploy
   ` + "`" + `

## Configuration

Update environment variables in the Render dashboard or modify render.yaml.

## Monitoring

Access logs and metrics in the Render dashboard:
https://dashboard.render.com
`
}

// ========================================
// Helper Functions
// ========================================

func filterApps(apps []AppInfo, serviceName string) []AppInfo {
	for _, app := range apps {
		if app.Name == serviceName {
			return []AppInfo{app}
		}
	}
	return apps
}

func normalizeEnvVar(name string) string {
	// Convert service name to uppercase env var format
	// e.g., "api-service" -> "API_SERVICE"
	result := ""
	for _, c := range name {
		if c == '-' {
			result += "_"
		} else {
			result += string(c)
		}
	}
	return result
}

// extractGitRepo extracts repository information from go.mod or config
func (g *Generator) extractGitRepo() string {
	// First check if specified in config
	if g.config.Deploy.DigitalOcean.GitRepo != "" {
		return g.config.Deploy.DigitalOcean.GitRepo
	}
	if g.config.Deploy.Render.GitRepo != "" {
		return g.config.Deploy.Render.GitRepo
	}
	
	// Try to extract from module path in config
	if g.config.Project.Module != "" {
		return extractRepoFromModule(g.config.Project.Module)
	}
	
	// Try to parse go.mod
	goModPath := filepath.Join(g.config.RootDir, "go.mod")
	if repo := parseGoModForRepo(goModPath); repo != "" {
		return repo
	}
	
	return "your-org/your-repo"
}

// extractRepoFromModule converts module path to repo format
// e.g., "github.com/myorg/myrepo" -> "myorg/myrepo"
func extractRepoFromModule(module string) string {
	// Handle github.com modules
	if strings.HasPrefix(module, "github.com/") {
		parts := strings.Split(strings.TrimPrefix(module, "github.com/"), "/")
		if len(parts) >= 2 {
			return parts[0] + "/" + parts[1]
		}
	}
	
	// Handle gitlab.com modules
	if strings.HasPrefix(module, "gitlab.com/") {
		parts := strings.Split(strings.TrimPrefix(module, "gitlab.com/"), "/")
		if len(parts) >= 2 {
			return parts[0] + "/" + parts[1]
		}
	}
	
	// Handle bitbucket.org modules
	if strings.HasPrefix(module, "bitbucket.org/") {
		parts := strings.Split(strings.TrimPrefix(module, "bitbucket.org/"), "/")
		if len(parts) >= 2 {
			return parts[0] + "/" + parts[1]
		}
	}
	
	return ""
}

// parseGoModForRepo parses go.mod file to extract module path
func parseGoModForRepo(goModPath string) string {
	file, err := os.Open(goModPath)
	if err != nil {
		return ""
	}
	defer file.Close()
	
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			module := strings.TrimSpace(strings.TrimPrefix(line, "module"))
			return extractRepoFromModule(module)
		}
	}
	
	return ""
}

// getGitBranch returns the git branch from config or default
func (g *Generator) getGitBranch() string {
	// Digital Ocean config
	if g.config.Deploy.DigitalOcean.GitBranch != "" {
		return g.config.Deploy.DigitalOcean.GitBranch
	}
	
	// Render config
	if g.config.Deploy.Render.GitBranch != "" {
		return g.config.Deploy.Render.GitBranch
	}
	
	// Default
	return "main"
}

// getDeployOnPush returns whether to deploy on push from config
func (g *Generator) getDeployOnPush() bool {
	// Check Digital Ocean config
	return g.config.Deploy.DigitalOcean.DeployOnPush
}

