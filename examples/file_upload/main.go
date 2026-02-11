package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	forge "github.com/xraph/forge"
)

// FileUploadRequest demonstrates multipart form handling
type FileUploadRequest struct {
	// Path parameter
	UserID string `path:"userId" description:"User ID" format:"uuid"`

	// Form fields (regular form data)
	Description string   `form:"description,omitempty" description:"File description" maxLength:"500"`
	Tags        []string `form:"tags,omitempty" description:"File tags"`

	// File field - binary format indicates file upload
	Avatar []byte `form:"avatar" description:"User avatar image" format:"binary"`
}

// MultiFileUploadRequest demonstrates multiple file uploads
type MultiFileUploadRequest struct {
	UserID string `path:"userId" description:"User ID" format:"uuid"`
	Folder string `form:"folder,omitempty" description:"Destination folder" maxLength:"100"`
}

// FileUploadResponse represents the response for file upload
type FileUploadResponse struct {
	FileID      string    `json:"fileId" description:"Unique file identifier"`
	Filename    string    `json:"filename" description:"Original filename"`
	Size        int64     `json:"size" description:"File size in bytes"`
	ContentType string    `json:"contentType" description:"MIME type"`
	Checksum    string    `json:"checksum" description:"SHA-256 checksum"`
	URL         string    `json:"url" description:"File access URL"`
	UploadedAt  time.Time `json:"uploadedAt" format:"date-time"`
}

// MultiFileUploadResponse represents response for multiple file uploads
type MultiFileUploadResponse struct {
	Files     []FileUploadResponse `json:"files" description:"Uploaded files"`
	TotalSize int64                `json:"totalSize" description:"Total size of all files"`
	Count     int                  `json:"count" description:"Number of files uploaded"`
}

// FileService handles file operations
type FileService struct {
	// In production, this would integrate with storage backends
	// like S3, Azure Blob Storage, or local filesystem
}

// Allowed file types for validation
var allowedImageTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
}

// Maximum file sizes
const (
	MaxAvatarSize   = 10 * 1024 * 1024  // 10MB
	MaxDocumentSize = 100 * 1024 * 1024 // 100MB
	MaxTotalSize    = 500 * 1024 * 1024 // 500MB for batch uploads
)

// UploadAvatar handles single file upload with validation
func (s *FileService) UploadAvatar(ctx forge.Context, req *FileUploadRequest) (*FileUploadResponse, error) {
	// Get the uploaded file
	file, header, err := ctx.FormFile("avatar")
	if err != nil {
		return nil, forge.BadRequest(fmt.Sprintf("failed to get file: %v", err))
	}
	defer file.Close()

	// Validate file size
	if header.Size > MaxAvatarSize {
		return nil, forge.BadRequest(fmt.Sprintf("file too large: %d bytes (max %d bytes)", header.Size, MaxAvatarSize))
	}

	if header.Size == 0 {
		return nil, forge.BadRequest("empty file not allowed")
	}

	// Validate content type
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		// Detect from filename extension
		contentType = mime.TypeByExtension(filepath.Ext(header.Filename))
	}

	if !allowedImageTypes[contentType] {
		return nil, forge.BadRequest(fmt.Sprintf("unsupported file type: %s (allowed: JPEG, PNG, GIF, WebP)", contentType))
	}

	// Read file data
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, forge.InternalError(fmt.Errorf("failed to read file: %w", err))
	}

	// Verify file type from magic bytes (first line of defense against spoofing)
	if !isValidImageType(data, contentType) {
		return nil, forge.BadRequest("file content does not match declared content type")
	}

	// Calculate checksum
	hash := sha256.Sum256(data)
	checksum := hex.EncodeToString(hash[:])

	// Sanitize filename
	safeFilename := sanitizeFilename(header.Filename)

	// Generate unique file ID
	fileID := generateFileID(req.UserID, safeFilename, checksum)

	// Get optional form values
	description := ctx.FormValue("description")
	tags := ctx.FormValues("tags")

	// In production: Save to storage backend
	// err = s.saveToStorage(ctx.Context(), fileID, data, contentType)
	log.Printf("Saving file: %s (user: %s, size: %d, checksum: %s)", safeFilename, req.UserID, len(data), checksum)
	log.Printf("Description: %s, Tags: %v", description, tags)

	// Simulate storage
	url := fmt.Sprintf("/files/%s/%s", req.UserID, fileID)

	return &FileUploadResponse{
		FileID:      fileID,
		Filename:    safeFilename,
		Size:        header.Size,
		ContentType: contentType,
		Checksum:    checksum,
		URL:         url,
		UploadedAt:  time.Now(),
	}, nil
}

// UploadMultipleFiles handles multiple file uploads
func (s *FileService) UploadMultipleFiles(ctx forge.Context, req *MultiFileUploadRequest) (*MultiFileUploadResponse, error) {
	// Parse multipart form with 32MB max memory
	if err := ctx.ParseMultipartForm(32 << 20); err != nil {
		return nil, forge.BadRequest(fmt.Sprintf("failed to parse form: %v", err))
	}

	// Get all uploaded files (field name: "documents")
	files, err := ctx.FormFiles("documents")
	if err != nil {
		return nil, forge.BadRequest(fmt.Sprintf("no files uploaded: %v", err))
	}

	if len(files) == 0 {
		return nil, forge.BadRequest("at least one file is required")
	}

	// Validate total size
	var totalSize int64
	for _, fileHeader := range files {
		totalSize += fileHeader.Size
	}

	if totalSize > MaxTotalSize {
		return nil, forge.BadRequest(fmt.Sprintf("total size too large: %d bytes (max %d bytes)", totalSize, MaxTotalSize))
	}

	// Process each file
	uploadedFiles := make([]FileUploadResponse, 0, len(files))

	for _, fileHeader := range files {
		// Open file
		file, err := fileHeader.Open()
		if err != nil {
			return nil, forge.InternalError(fmt.Errorf("failed to open file %s: %w", fileHeader.Filename, err))
		}

		// Read file data
		data, err := io.ReadAll(file)
		file.Close()
		if err != nil {
			return nil, forge.InternalError(fmt.Errorf("failed to read file %s: %w", fileHeader.Filename, err))
		}

		// Detect content type
		contentType := fileHeader.Header.Get("Content-Type")
		if contentType == "" {
			contentType = mime.TypeByExtension(filepath.Ext(fileHeader.Filename))
		}
		if contentType == "" {
			contentType = "application/octet-stream"
		}

		// Calculate checksum
		hash := sha256.Sum256(data)
		checksum := hex.EncodeToString(hash[:])

		// Sanitize filename
		safeFilename := sanitizeFilename(fileHeader.Filename)

		// Generate file ID
		fileID := generateFileID(req.UserID, safeFilename, checksum)

		// In production: Save to storage backend
		log.Printf("Saving file: %s (size: %d, checksum: %s)", safeFilename, len(data), checksum)

		url := fmt.Sprintf("/files/%s/%s/%s", req.UserID, req.Folder, fileID)

		uploadedFiles = append(uploadedFiles, FileUploadResponse{
			FileID:      fileID,
			Filename:    safeFilename,
			Size:        fileHeader.Size,
			ContentType: contentType,
			Checksum:    checksum,
			URL:         url,
			UploadedAt:  time.Now(),
		})
	}

	return &MultiFileUploadResponse{
		Files:     uploadedFiles,
		TotalSize: totalSize,
		Count:     len(uploadedFiles),
	}, nil
}

// Helper functions

// isValidImageType verifies file content matches declared type using magic bytes
func isValidImageType(data []byte, contentType string) bool {
	if len(data) < 12 {
		return false
	}

	switch contentType {
	case "image/jpeg":
		return data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF
	case "image/png":
		return data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47
	case "image/gif":
		return string(data[0:6]) == "GIF87a" || string(data[0:6]) == "GIF89a"
	case "image/webp":
		return string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP"
	default:
		return false
	}
}

// sanitizeFilename removes dangerous characters from filename
func sanitizeFilename(filename string) string {
	// Remove path components
	filename = filepath.Base(filename)

	// Replace dangerous characters
	replacer := strings.NewReplacer(
		"..", "_",
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
	)
	filename = replacer.Replace(filename)

	// Limit length
	if len(filename) > 255 {
		ext := filepath.Ext(filename)
		name := filename[:255-len(ext)]
		filename = name + ext
	}

	return filename
}

// generateFileID creates a unique file identifier
func generateFileID(userID, filename, checksum string) string {
	timestamp := time.Now().Unix()
	data := fmt.Sprintf("%s:%s:%s:%d", userID, filename, checksum, timestamp)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:16]) // Use first 16 bytes (32 hex chars)
}

func main() {
	// Create Forge app
	app := forge.NewApp(forge.AppConfig{
		Name:        "file-upload-example",
		Version:     "1.0.0",
		Description: "Comprehensive file upload example with validation and multipart form support",
		Environment: "development",
		HTTPAddress: ":8085",
		RouterOptions: []forge.RouterOption{
			forge.WithOpenAPI(forge.OpenAPIConfig{
				Title:       "File Upload API",
				Version:     "1.0.0",
				Description: "Production-ready file upload API with validation, multipart forms, and security best practices",
				// Note: Localhost server is automatically added based on HTTPAddress
			}),
		},
	})

	// Register file service
	forge.RegisterSingleton(app.Container(), "fileService", func(c forge.Container) (*FileService, error) {
		return &FileService{}, nil
	})

	// Get router
	router := app.Router()

	// Single file upload endpoint
	router.POST("/users/:userId/avatar", (*FileService).UploadAvatar,
		forge.WithName("uploadAvatar"),
		forge.WithSummary("Upload user avatar"),
		forge.WithDescription("Upload a user avatar image. Supports JPEG, PNG, GIF, and WebP formats. Maximum size: 10MB."),
		forge.WithTags("files", "users"),
		forge.WithRequestSchema(&FileUploadRequest{}),
		forge.WithFileUploadResponse(http.StatusOK),
		forge.WithErrorResponses(),
		forge.WithResponseSchema(http.StatusBadRequest, "Invalid file", &forge.HTTPError{}),
	)

	// Multiple file upload endpoint
	router.POST("/users/:userId/documents", (*FileService).UploadMultipleFiles,
		forge.WithName("uploadDocuments"),
		forge.WithSummary("Upload multiple documents"),
		forge.WithDescription("Upload multiple files in a single request. Maximum total size: 500MB."),
		forge.WithTags("files", "documents"),
		forge.WithRequestSchema(&MultiFileUploadRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Success", &MultiFileUploadResponse{}),
		forge.WithErrorResponses(),
	)

	// Start the app
	log.Printf("Starting file upload example on http://localhost:8085")
	log.Printf("OpenAPI documentation: http://localhost:8085/docs")
	log.Printf("\nExample curl commands:")
	log.Printf("  # Single file upload:")
	log.Printf("  curl -X POST http://localhost:8085/users/user-123/avatar \\")
	log.Printf("    -F 'avatar=@/path/to/image.jpg' \\")
	log.Printf("    -F 'description=Profile picture' \\")
	log.Printf("    -F 'tags=profile' \\")
	log.Printf("    -F 'tags=avatar'")
	log.Printf("")
	log.Printf("  # Multiple files upload:")
	log.Printf("  curl -X POST http://localhost:8085/users/user-123/documents \\")
	log.Printf("    -F 'documents=@/path/to/doc1.pdf' \\")
	log.Printf("    -F 'documents=@/path/to/doc2.pdf' \\")
	log.Printf("    -F 'folder=contracts'")

	if err := app.Start(context.Background()); err != nil {
		log.Fatalf("Failed to start app: %v", err)
	}
}
