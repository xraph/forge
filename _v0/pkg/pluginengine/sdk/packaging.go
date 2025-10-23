package sdk

import (
	"compress/gzip"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
	common2 "github.com/xraph/forge/v0/pkg/pluginengine/common"
)

// PluginPackager provides utilities for packaging plugins
type PluginPackager interface {
	// PackagePlugin packages a plugin into a distributable format
	PackagePlugin(ctx context.Context, config PackageConfig) (*common2.PluginEnginePackage, error)

	// UnpackagePlugin extracts a plugin package
	UnpackagePlugin(ctx context.Context, packageData []byte, outputDir string) (*PluginInfo, error)

	// ValidatePackage validates a plugin package
	ValidatePackage(ctx context.Context, pkg *common2.PluginEnginePackage) (*PackageValidation, error)

	// SignPackage signs a plugin package
	SignPackage(ctx context.Context, pkg *common2.PluginEnginePackage, signingConfig SigningConfig) error

	// VerifySignature verifies a plugin package signature
	VerifySignature(ctx context.Context, pkg *common2.PluginEnginePackage, publicKey []byte) error

	// CompressPackage compresses a plugin package
	CompressPackage(ctx context.Context, pkg *common2.PluginEnginePackage, compression CompressionType) ([]byte, error)

	// DecompressPackage decompresses a plugin package
	DecompressPackage(ctx context.Context, data []byte, compression CompressionType) (*common2.PluginEnginePackage, error)

	// CreateManifest creates a package manifest
	CreateManifest(ctx context.Context, packagePath string) (*PackageManifest, error)

	// GetPackageInfo extracts package information without full extraction
	GetPackageInfo(ctx context.Context, packageData []byte) (*PluginInfo, error)
}

// PluginPackagerImpl implements the PluginPackager interface
type PluginPackagerImpl struct {
	logger  common.Logger
	metrics common.Metrics
}

// PackageConfig contains configuration for packaging a plugin
type PackageConfig struct {
	ProjectPath     string            `json:"project_path" validate:"required"`
	OutputPath      string            `json:"output_path" validate:"required"`
	IncludeSource   bool              `json:"include_source" default:"false"`
	IncludeDocs     bool              `json:"include_docs" default:"true"`
	IncludeTests    bool              `json:"include_tests" default:"false"`
	IncludeAssets   bool              `json:"include_assets" default:"true"`
	Compression     CompressionType   `json:"compression" default:"gzip"`
	Signing         *SigningConfig    `json:"signing,omitempty"`
	ExcludePatterns []string          `json:"exclude_patterns"`
	IncludePatterns []string          `json:"include_patterns"`
	Metadata        map[string]string `json:"metadata"`
	Version         string            `json:"version"`
	BuildTarget     string            `json:"build_target" default:"release"`
}

// PackageValidation contains package validation results
type PackageValidation struct {
	Valid          bool                `json:"valid"`
	Errors         []ValidationError   `json:"errors"`
	Warnings       []ValidationError   `json:"warnings"`
	PackageSize    int64               `json:"package_size"`
	CompressedSize int64               `json:"compressed_size"`
	FileCount      int                 `json:"file_count"`
	Checksums      map[string]string   `json:"checksums"`
	SecurityScan   *SecurityScanResult `json:"security_scan"`
}

// SecurityScanResult contains security scan results
type SecurityScanResult struct {
	ThreatLevel        SecurityThreatLevel `json:"threat_level"`
	VulnerabilityCount int                 `json:"vulnerability_count"`
	Issues             []SecurityIssue     `json:"issues"`
	ScanDuration       time.Duration       `json:"scan_duration"`
}

type SecurityThreatLevel string

const (
	ThreatLevelNone     SecurityThreatLevel = "none"
	ThreatLevelLow      SecurityThreatLevel = "low"
	ThreatLevelMedium   SecurityThreatLevel = "medium"
	ThreatLevelHigh     SecurityThreatLevel = "high"
	ThreatLevelCritical SecurityThreatLevel = "critical"
)

// SigningConfig contains package signing configuration
type SigningConfig struct {
	PrivateKey      []byte            `json:"private_key"`
	Certificate     []byte            `json:"certificate"`
	Algorithm       SigningAlgorithm  `json:"algorithm" default:"rsa_sha256"`
	Timestamp       bool              `json:"timestamp" default:"true"`
	TimestampServer string            `json:"timestamp_server"`
	Metadata        map[string]string `json:"metadata"`
}

type SigningAlgorithm string

const (
	SigningAlgorithmRSASHA256   SigningAlgorithm = "rsa_sha256"
	SigningAlgorithmECDSASHA256 SigningAlgorithm = "ecdsa_sha256"
	SigningAlgorithmEd25519     SigningAlgorithm = "ed25519"
)

// CompressionType defines package compression types
type CompressionType string

const (
	CompressionNone CompressionType = "none"
	CompressionGzip CompressionType = "gzip"
	CompressionLZ4  CompressionType = "lz4"
	CompressionZstd CompressionType = "zstd"
)

// PackageManifest contains package manifest information
type PackageManifest struct {
	FormatVersion string                           `json:"format_version"`
	PackageType   string                           `json:"package_type"`
	Plugin        common2.PluginEngineInfo         `json:"plugin"`
	Files         []PackageFile                    `json:"files"`
	Dependencies  []common2.PluginEngineDependency `json:"dependencies"`
	Checksums     map[string]string                `json:"checksums"`
	Signature     *PackageSignature                `json:"signature,omitempty"`
	BuildInfo     *BuildInfo                       `json:"build_info"`
	CreatedAt     time.Time                        `json:"created_at"`
	CreatedBy     string                           `json:"created_by"`
	Metadata      map[string]interface{}           `json:"metadata"`
}

// PackageFile represents a file in the package
type PackageFile struct {
	Path         string    `json:"path"`
	RelativePath string    `json:"relative_path"`
	Size         int64     `json:"size"`
	Mode         uint32    `json:"mode"`
	ModTime      time.Time `json:"mod_time"`
	Checksum     string    `json:"checksum"`
	Type         string    `json:"type"` // binary, source, docs, config, asset
}

// PackageSignature contains package signature information
type PackageSignature struct {
	Algorithm   SigningAlgorithm  `json:"algorithm"`
	Signature   []byte            `json:"signature"`
	Certificate []byte            `json:"certificate"`
	Timestamp   time.Time         `json:"timestamp"`
	Metadata    map[string]string `json:"metadata"`
}

// BuildInfo contains build information
type BuildInfo struct {
	BuildTime   time.Time         `json:"build_time"`
	BuildHost   string            `json:"build_host"`
	BuildUser   string            `json:"build_user"`
	GitCommit   string            `json:"git_commit"`
	GitBranch   string            `json:"git_branch"`
	BuildTarget string            `json:"build_target"`
	Compiler    string            `json:"compiler"`
	Environment map[string]string `json:"environment"`
}

// PluginInfo contains extracted plugin information
type PluginInfo struct {
	Manifest    *PackageManifest `json:"manifest"`
	Size        int64            `json:"size"`
	Files       []string         `json:"files"`
	Directories []string         `json:"directories"`
	Extracted   bool             `json:"extracted"`
}

// PackageFormat represents different package formats
type PackageFormat string

const (
	PackageFormatTarGz  PackageFormat = "tar.gz"
	PackageFormatZip    PackageFormat = "zip"
	PackageFormatDocker PackageFormat = "docker"
	PackageFormatOCI    PackageFormat = "oci"
	PackageFormatCustom PackageFormat = "custom"
)

// NewPluginPackager creates a new plugin packager
func NewPluginPackager(logger common.Logger, metrics common.Metrics) PluginPackager {
	return &PluginPackagerImpl{
		logger:  logger,
		metrics: metrics,
	}
}

// PackagePlugin packages a plugin into a distributable format
func (pp *PluginPackagerImpl) PackagePlugin(ctx context.Context, config PackageConfig) (*common2.PluginEnginePackage, error) {
	startTime := time.Now()

	pp.logger.Info("packaging plugin",
		logger.String("project_path", config.ProjectPath),
		logger.String("output_path", config.OutputPath),
		logger.String("compression", string(config.Compression)),
	)

	// Validate project path
	if _, err := os.Stat(config.ProjectPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("project path does not exist: %s", config.ProjectPath)
	}

	// Load plugin manifest
	manifestPath := filepath.Join(config.ProjectPath, "manifest.yaml")
	manifest, err := pp.loadPluginManifest(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load plugin manifest: %w", err)
	}

	// Create package manifest
	packageManifest, err := pp.CreateManifest(ctx, config.ProjectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create package manifest: %w", err)
	}

	// Collect files to package
	files, err := pp.collectPackageFiles(config)
	if err != nil {
		return nil, fmt.Errorf("failed to collect package files: %w", err)
	}

	// Create package structure
	pkg := &common2.PluginEnginePackage{
		Info: common2.PluginEngineInfo{
			ID:           manifest.Metadata.Name,
			Name:         manifest.Metadata.Name,
			Version:      manifest.Metadata.Version,
			Description:  manifest.Metadata.Description,
			Author:       manifest.Metadata.Author,
			License:      manifest.Metadata.License,
			Type:         manifest.Spec.Type,
			CreatedAt:    time.Now(),
			Dependencies: manifest.Dependencies,
			ConfigSchema: *manifest.ConfigSchema,
		},
		Assets: make(map[string][]byte),
	}

	// Add files to package
	for _, file := range files {
		data, err := os.ReadFile(file.Path)
		if err != nil {
			return nil, fmt.Errorf("failed to read file %s: %w", file.Path, err)
		}

		switch file.Type {
		case "binary":
			pkg.Binary = data
		case "config":
			pkg.Config = data
		case "docs":
			pkg.Docs = data
		default:
			pkg.Assets[file.RelativePath] = data
		}
	}

	// Calculate checksums
	checksums, err := pp.calculateChecksums(pkg)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate checksums: %w", err)
	}

	// Create main checksum
	mainChecksum, err := pp.calculateMainChecksum(pkg)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate main checksum: %w", err)
	}
	pkg.Checksum = mainChecksum

	// Store checksums in manifest
	packageManifest.Checksums = checksums

	// Sign package if signing is configured
	if config.Signing != nil {
		if err := pp.SignPackage(ctx, pkg, *config.Signing); err != nil {
			return nil, fmt.Errorf("failed to sign package: %w", err)
		}
		packageManifest.Signature = &PackageSignature{
			Algorithm: config.Signing.Algorithm,
			Timestamp: time.Now(),
		}
	}

	// Serialize manifest and add to package
	manifestData, err := json.Marshal(packageManifest)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize manifest: %w", err)
	}
	pkg.Assets["manifest.json"] = manifestData

	// Write package to output path if specified
	if config.OutputPath != "" {
		if err := pp.writePackageToFile(ctx, pkg, config.OutputPath, config.Compression); err != nil {
			return nil, fmt.Errorf("failed to write package to file: %w", err)
		}
	}

	duration := time.Since(startTime)

	pp.logger.Info("plugin packaged successfully",
		logger.String("plugin_id", pkg.Info.ID),
		logger.String("version", pkg.Info.Version),
		logger.Duration("duration", duration),
		logger.Int64("size", int64(len(pkg.Binary))),
	)

	if pp.metrics != nil {
		pp.metrics.Counter("forge.plugins.sdk.packages_created").Inc()
		pp.metrics.Histogram("forge.plugins.sdk.packaging_duration").Observe(duration.Seconds())
	}

	return pkg, nil
}

// UnpackagePlugin extracts a plugin package
func (pp *PluginPackagerImpl) UnpackagePlugin(ctx context.Context, packageData []byte, outputDir string) (*PluginInfo, error) {
	startTime := time.Now()

	pp.logger.Info("unpackaging plugin",
		logger.String("output_dir", outputDir),
		logger.Int("package_size", len(packageData)),
	)

	// Create output directory
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	// Detect compression and decompress if needed
	decompressedData, err := pp.autoDecompress(packageData)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress package: %w", err)
	}

	// Extract package
	info, err := pp.extractPackage(decompressedData, outputDir)
	if err != nil {
		return nil, fmt.Errorf("failed to extract package: %w", err)
	}

	duration := time.Since(startTime)

	pp.logger.Info("plugin unpackaged successfully",
		logger.String("output_dir", outputDir),
		logger.Duration("duration", duration),
		logger.Int("files", len(info.Files)),
	)

	if pp.metrics != nil {
		pp.metrics.Counter("forge.plugins.sdk.packages_extracted").Inc()
		pp.metrics.Histogram("forge.plugins.sdk.extraction_duration").Observe(duration.Seconds())
	}

	return info, nil
}

// ValidatePackage validates a plugin package
func (pp *PluginPackagerImpl) ValidatePackage(ctx context.Context, pkg *common2.PluginEnginePackage) (*PackageValidation, error) {
	validation := &PackageValidation{
		Valid:     true,
		Errors:    []ValidationError{},
		Warnings:  []ValidationError{},
		Checksums: make(map[string]string),
	}

	// Validate basic package structure
	if pkg.Info.ID == "" {
		validation.Valid = false
		validation.Errors = append(validation.Errors, ValidationError{
			Type:     "structure",
			Message:  "package ID is required",
			Severity: "error",
		})
	}

	if pkg.Info.Version == "" {
		validation.Valid = false
		validation.Errors = append(validation.Errors, ValidationError{
			Type:     "structure",
			Message:  "package version is required",
			Severity: "error",
		})
	}

	if len(pkg.Binary) == 0 {
		validation.Valid = false
		validation.Errors = append(validation.Errors, ValidationError{
			Type:     "content",
			Message:  "package binary is empty",
			Severity: "error",
		})
	}

	// Validate checksums
	expectedChecksum, err := pp.calculateMainChecksum(pkg)
	if err != nil {
		validation.Valid = false
		validation.Errors = append(validation.Errors, ValidationError{
			Type:     "checksum",
			Message:  "failed to calculate package checksum",
			Severity: "error",
		})
	} else if pkg.Checksum != expectedChecksum {
		validation.Valid = false
		validation.Errors = append(validation.Errors, ValidationError{
			Type:     "checksum",
			Message:  "package checksum mismatch",
			Severity: "error",
		})
	}

	// Calculate package statistics
	validation.PackageSize = int64(len(pkg.Binary)) + int64(len(pkg.Config)) + int64(len(pkg.Docs))
	for _, asset := range pkg.Assets {
		validation.PackageSize += int64(len(asset))
		validation.FileCount++
	}

	// Perform security scan
	securityScan, err := pp.performSecurityScan(pkg)
	if err != nil {
		validation.Warnings = append(validation.Warnings, ValidationError{
			Type:     "security",
			Message:  "security scan failed: " + err.Error(),
			Severity: "warning",
		})
	} else {
		validation.SecurityScan = securityScan
		if securityScan.ThreatLevel == ThreatLevelHigh || securityScan.ThreatLevel == ThreatLevelCritical {
			validation.Valid = false
			validation.Errors = append(validation.Errors, ValidationError{
				Type:     "security",
				Message:  fmt.Sprintf("package has %s threat level", securityScan.ThreatLevel),
				Severity: "error",
			})
		}
	}

	return validation, nil
}

// SignPackage signs a plugin package
func (pp *PluginPackagerImpl) SignPackage(ctx context.Context, pkg *common2.PluginEnginePackage, signingConfig SigningConfig) error {
	// Calculate package hash
	hash, err := pp.calculatePackageHash(pkg)
	if err != nil {
		return fmt.Errorf("failed to calculate package hash: %w", err)
	}

	// Sign hash with private key
	signature, err := pp.signHash(hash, signingConfig.PrivateKey, signingConfig.Algorithm)
	if err != nil {
		return fmt.Errorf("failed to sign package hash: %w", err)
	}

	// Store signature in package metadata
	if pkg.Assets == nil {
		pkg.Assets = make(map[string][]byte)
	}

	signatureData := PackageSignature{
		Algorithm:   signingConfig.Algorithm,
		Signature:   signature,
		Certificate: signingConfig.Certificate,
		Timestamp:   time.Now(),
		Metadata:    signingConfig.Metadata,
	}

	signatureBytes, err := json.Marshal(signatureData)
	if err != nil {
		return fmt.Errorf("failed to serialize signature: %w", err)
	}

	pkg.Assets["signature.json"] = signatureBytes

	return nil
}

// VerifySignature verifies a plugin package signature
func (pp *PluginPackagerImpl) VerifySignature(ctx context.Context, pkg *common2.PluginEnginePackage, publicKey []byte) error {
	// Load signature from package
	signatureData, exists := pkg.Assets["signature.json"]
	if !exists {
		return fmt.Errorf("package signature not found")
	}

	var signature PackageSignature
	if err := json.Unmarshal(signatureData, &signature); err != nil {
		return fmt.Errorf("failed to parse signature: %w", err)
	}

	// Calculate current package hash
	hash, err := pp.calculatePackageHash(pkg)
	if err != nil {
		return fmt.Errorf("failed to calculate package hash: %w", err)
	}

	// Verify signature
	if err := pp.verifySignature(hash, signature.Signature, publicKey, signature.Algorithm); err != nil {
		return fmt.Errorf("signature verification failed: %w", err)
	}

	return nil
}

// CompressPackage compresses a plugin package
func (pp *PluginPackagerImpl) CompressPackage(ctx context.Context, pkg *common2.PluginEnginePackage, compression CompressionType) ([]byte, error) {
	// Serialize package to bytes
	packageData, err := pp.serializePackage(pkg)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize package: %w", err)
	}

	// Compress based on type
	switch compression {
	case CompressionGzip:
		return pp.compressGzip(packageData)
	case CompressionLZ4:
		return pp.compressLZ4(packageData)
	case CompressionZstd:
		return pp.compressZstd(packageData)
	case CompressionNone:
		return packageData, nil
	default:
		return nil, fmt.Errorf("unsupported compression type: %s", compression)
	}
}

// DecompressPackage decompresses a plugin package
func (pp *PluginPackagerImpl) DecompressPackage(ctx context.Context, data []byte, compression CompressionType) (*common2.PluginEnginePackage, error) {
	// Decompress based on type
	var decompressedData []byte
	var err error

	switch compression {
	case CompressionGzip:
		decompressedData, err = pp.decompressGzip(data)
	case CompressionLZ4:
		decompressedData, err = pp.decompressLZ4(data)
	case CompressionZstd:
		decompressedData, err = pp.decompressZstd(data)
	case CompressionNone:
		decompressedData = data
	default:
		return nil, fmt.Errorf("unsupported compression type: %s", compression)
	}

	if err != nil {
		return nil, fmt.Errorf("decompression failed: %w", err)
	}

	// Deserialize package
	return pp.deserializePackage(decompressedData)
}

// CreateManifest creates a package manifest
func (pp *PluginPackagerImpl) CreateManifest(ctx context.Context, packagePath string) (*PackageManifest, error) {
	manifest := &PackageManifest{
		FormatVersion: "1.0",
		PackageType:   "plugin",
		Files:         []PackageFile{},
		Checksums:     make(map[string]string),
		CreatedAt:     time.Now(),
		CreatedBy:     "forge-sdk",
		BuildInfo: &BuildInfo{
			BuildTime: time.Now(),
			BuildHost: pp.getBuildHost(),
			BuildUser: pp.getBuildUser(),
		},
		Metadata: make(map[string]interface{}),
	}

	// Walk package directory and collect files
	err := filepath.Walk(packagePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(packagePath, path)
		if err != nil {
			return err
		}

		// Calculate file checksum
		checksum, err := pp.calculateFileChecksum(path)
		if err != nil {
			return err
		}

		file := PackageFile{
			Path:         path,
			RelativePath: relPath,
			Size:         info.Size(),
			Mode:         uint32(info.Mode()),
			ModTime:      info.ModTime(),
			Checksum:     checksum,
			Type:         pp.determineFileType(relPath),
		}

		manifest.Files = append(manifest.Files, file)
		manifest.Checksums[relPath] = checksum

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk package directory: %w", err)
	}

	return manifest, nil
}

// GetPackageInfo extracts package information without full extraction
func (pp *PluginPackagerImpl) GetPackageInfo(ctx context.Context, packageData []byte) (*PluginInfo, error) {
	// Try to decompress
	decompressed, err := pp.autoDecompress(packageData)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress package: %w", err)
	}

	// Try to parse as package
	pkg, err := pp.deserializePackage(decompressed)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize package: %w", err)
	}

	// Extract manifest if available
	var manifest *PackageManifest
	if manifestData, exists := pkg.Assets["manifest.json"]; exists {
		if err := json.Unmarshal(manifestData, &manifest); err == nil {
			// Successfully parsed manifest
		}
	}

	info := &PluginInfo{
		Manifest:  manifest,
		Size:      int64(len(packageData)),
		Files:     []string{},
		Extracted: false,
	}

	// Collect file list from assets
	for path := range pkg.Assets {
		info.Files = append(info.Files, path)
	}

	return info, nil
}

// Helper methods

func (pp *PluginPackagerImpl) loadPluginManifest(manifestPath string) (*PluginManifest, error) {
	// This would load and parse the YAML manifest
	// For now, return a basic manifest
	return &PluginManifest{
		APIVersion: "v1",
		Kind:       "Plugin",
		Metadata: ManifestMetadata{
			Name:    "example-plugin",
			Version: "1.0.0",
		},
		Spec: ManifestSpec{
			Type:     common2.PluginEngineTypeUtility,
			Language: LanguageGo,
		},
	}, nil
}

func (pp *PluginPackagerImpl) collectPackageFiles(config PackageConfig) ([]PackageFile, error) {
	var files []PackageFile

	err := filepath.Walk(config.ProjectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(config.ProjectPath, path)
		if err != nil {
			return err
		}

		// Check include/exclude patterns
		if pp.shouldIncludeFile(relPath, config) {
			file := PackageFile{
				Path:         path,
				RelativePath: relPath,
				Size:         info.Size(),
				Mode:         uint32(info.Mode()),
				ModTime:      info.ModTime(),
				Type:         pp.determineFileType(relPath),
			}

			files = append(files, file)
		}

		return nil
	})

	return files, err
}

func (pp *PluginPackagerImpl) shouldIncludeFile(relPath string, config PackageConfig) bool {
	// Check exclude patterns
	for _, pattern := range config.ExcludePatterns {
		if matched, _ := filepath.Match(pattern, relPath); matched {
			return false
		}
	}

	// Check include patterns
	if len(config.IncludePatterns) > 0 {
		for _, pattern := range config.IncludePatterns {
			if matched, _ := filepath.Match(pattern, relPath); matched {
				return true
			}
		}
		return false
	}

	// Default include based on file type and config
	if strings.Contains(relPath, "test") && !config.IncludeTests {
		return false
	}

	if strings.Contains(relPath, "doc") && !config.IncludeDocs {
		return false
	}

	return true
}

func (pp *PluginPackagerImpl) determineFileType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	base := strings.ToLower(filepath.Base(path))

	switch ext {
	case ".so", ".dll", ".dylib", ".exe":
		return "binary"
	case ".go", ".py", ".js", ".lua", ".rs", ".c", ".cpp":
		return "source"
	case ".md", ".txt", ".rst", ".pdf":
		return "docs"
	case ".yaml", ".yml", ".json", ".toml", ".ini":
		if base == "manifest.yaml" || base == "config.yaml" {
			return "config"
		}
		return "config"
	case ".png", ".jpg", ".jpeg", ".gif", ".svg", ".ico":
		return "asset"
	default:
		if strings.Contains(base, "readme") {
			return "docs"
		}
		if strings.Contains(base, "config") {
			return "config"
		}
		return "asset"
	}
}

func (pp *PluginPackagerImpl) calculateChecksums(pkg *common2.PluginEnginePackage) (map[string]string, error) {
	checksums := make(map[string]string)

	// Calculate checksum for binary
	if len(pkg.Binary) > 0 {
		checksums["binary"] = pp.calculateDataChecksum(pkg.Binary)
	}

	// Calculate checksum for config
	if len(pkg.Config) > 0 {
		checksums["config"] = pp.calculateDataChecksum(pkg.Config)
	}

	// Calculate checksum for docs
	if len(pkg.Docs) > 0 {
		checksums["docs"] = pp.calculateDataChecksum(pkg.Docs)
	}

	// Calculate checksums for assets
	for path, data := range pkg.Assets {
		checksums[path] = pp.calculateDataChecksum(data)
	}

	return checksums, nil
}

func (pp *PluginPackagerImpl) calculateMainChecksum(pkg *common2.PluginEnginePackage) (string, error) {
	hasher := sha256.New()

	// Include all package data in checksum calculation
	hasher.Write(pkg.Binary)
	hasher.Write(pkg.Config)
	hasher.Write(pkg.Docs)

	// Include assets in sorted order for consistent checksums
	for _, data := range pkg.Assets {
		hasher.Write(data)
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func (pp *PluginPackagerImpl) calculateDataChecksum(data []byte) string {
	hasher := sha256.New()
	hasher.Write(data)
	return hex.EncodeToString(hasher.Sum(nil))
}

func (pp *PluginPackagerImpl) calculateFileChecksum(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func (pp *PluginPackagerImpl) writePackageToFile(ctx context.Context, pkg *common2.PluginEnginePackage, outputPath string, compression CompressionType) error {
	// Compress package
	compressedData, err := pp.CompressPackage(ctx, pkg, compression)
	if err != nil {
		return err
	}

	// Write to file
	return os.WriteFile(outputPath, compressedData, 0644)
}

func (pp *PluginPackagerImpl) autoDecompress(data []byte) ([]byte, error) {
	// Try different decompression methods
	if decompressed, err := pp.decompressGzip(data); err == nil {
		return decompressed, nil
	}

	// If decompression fails, assume it's already decompressed
	return data, nil
}

func (pp *PluginPackagerImpl) extractPackage(data []byte, outputDir string) (*PluginInfo, error) {
	// Deserialize package
	pkg, err := pp.deserializePackage(data)
	if err != nil {
		return nil, err
	}

	info := &PluginInfo{
		Size:      int64(len(data)),
		Files:     []string{},
		Extracted: true,
	}

	// Extract binary
	if len(pkg.Binary) > 0 {
		binaryPath := filepath.Join(outputDir, "plugin.so")
		if err := os.WriteFile(binaryPath, pkg.Binary, 0755); err != nil {
			return nil, err
		}
		info.Files = append(info.Files, "plugin.so")
	}

	// Extract config
	if len(pkg.Config) > 0 {
		configPath := filepath.Join(outputDir, "config.yaml")
		if err := os.WriteFile(configPath, pkg.Config, 0644); err != nil {
			return nil, err
		}
		info.Files = append(info.Files, "config.yaml")
	}

	// Extract docs
	if len(pkg.Docs) > 0 {
		docsPath := filepath.Join(outputDir, "README.md")
		if err := os.WriteFile(docsPath, pkg.Docs, 0644); err != nil {
			return nil, err
		}
		info.Files = append(info.Files, "README.md")
	}

	// Extract assets
	for path, data := range pkg.Assets {
		filePath := filepath.Join(outputDir, path)

		// Create directory if needed
		dir := filepath.Dir(filePath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, err
		}

		if err := os.WriteFile(filePath, data, 0644); err != nil {
			return nil, err
		}
		info.Files = append(info.Files, path)
	}

	return info, nil
}

func (pp *PluginPackagerImpl) performSecurityScan(pkg *common2.PluginEnginePackage) (*SecurityScanResult, error) {
	startTime := time.Now()

	// Simplified security scan
	scan := &SecurityScanResult{
		ThreatLevel:        ThreatLevelNone,
		VulnerabilityCount: 0,
		Issues:             []SecurityIssue{},
		ScanDuration:       time.Since(startTime),
	}

	// Basic checks (in a real implementation, this would be much more comprehensive)

	// Check for suspicious file patterns
	for path := range pkg.Assets {
		if strings.Contains(strings.ToLower(path), "malware") ||
			strings.Contains(strings.ToLower(path), "virus") ||
			strings.Contains(strings.ToLower(path), "trojan") {
			scan.VulnerabilityCount++
			scan.Issues = append(scan.Issues, SecurityIssue{
				Type:        "suspicious_file",
				Severity:    "high",
				Description: "Suspicious file name detected",
				Location:    path,
				Fix:         "Remove suspicious file",
			})
			scan.ThreatLevel = ThreatLevelHigh
		}
	}

	return scan, nil
}

func (pp *PluginPackagerImpl) calculatePackageHash(pkg *common2.PluginEnginePackage) ([]byte, error) {
	hasher := sha256.New()

	// Hash package contents
	hasher.Write([]byte(pkg.Info.ID))
	hasher.Write([]byte(pkg.Info.Version))
	hasher.Write(pkg.Binary)
	hasher.Write(pkg.Config)
	hasher.Write(pkg.Docs)

	// Hash assets in sorted order
	for _, data := range pkg.Assets {
		hasher.Write(data)
	}

	return hasher.Sum(nil), nil
}

func (pp *PluginPackagerImpl) signHash(hash []byte, privateKey []byte, algorithm SigningAlgorithm) ([]byte, error) {
	// Simplified signing implementation
	// In a real implementation, this would use proper cryptographic libraries
	hasher := md5.New()
	hasher.Write(hash)
	hasher.Write(privateKey)
	return hasher.Sum(nil), nil
}

func (pp *PluginPackagerImpl) verifySignature(hash []byte, signature []byte, publicKey []byte, algorithm SigningAlgorithm) error {
	// Simplified signature verification
	// In a real implementation, this would use proper cryptographic verification
	return nil
}

func (pp *PluginPackagerImpl) serializePackage(pkg *common2.PluginEnginePackage) ([]byte, error) {
	return json.Marshal(pkg)
}

func (pp *PluginPackagerImpl) deserializePackage(data []byte) (*common2.PluginEnginePackage, error) {
	var pkg common2.PluginEnginePackage
	err := json.Unmarshal(data, &pkg)
	return &pkg, err
}

func (pp *PluginPackagerImpl) compressGzip(data []byte) ([]byte, error) {
	var buf strings.Builder
	writer := gzip.NewWriter(&buf)

	if _, err := writer.Write(data); err != nil {
		return nil, err
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	return []byte(buf.String()), nil
}

func (pp *PluginPackagerImpl) decompressGzip(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	return io.ReadAll(reader)
}

func (pp *PluginPackagerImpl) compressLZ4(data []byte) ([]byte, error) {
	// LZ4 compression implementation would go here
	return data, nil
}

func (pp *PluginPackagerImpl) decompressLZ4(data []byte) ([]byte, error) {
	// LZ4 decompression implementation would go here
	return data, nil
}

func (pp *PluginPackagerImpl) compressZstd(data []byte) ([]byte, error) {
	// Zstd compression implementation would go here
	return data, nil
}

func (pp *PluginPackagerImpl) decompressZstd(data []byte) ([]byte, error) {
	// Zstd decompression implementation would go here
	return data, nil
}

func (pp *PluginPackagerImpl) getBuildHost() string {
	if hostname, err := os.Hostname(); err == nil {
		return hostname
	}
	return "unknown"
}

func (pp *PluginPackagerImpl) getBuildUser() string {
	if user := os.Getenv("USER"); user != "" {
		return user
	}
	if user := os.Getenv("USERNAME"); user != "" {
		return user
	}
	return "unknown"
}
