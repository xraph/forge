# File Upload Example

This example demonstrates production-ready file upload handling in Forge with multipart form support, comprehensive validation, and security best practices.

## Features

- **Multipart Form Support**: Native handling of `multipart/form-data` requests
- **File Validation**: Size limits, content type verification, magic byte checking
- **Multiple File Uploads**: Batch upload with aggregate size limits
- **Security**: Filename sanitization, content-type verification, checksum generation
- **OpenAPI Integration**: Automatic API documentation with proper multipart/form-data schemas

## Running the Example

```bash
cd examples/file_upload
go run main.go
```

The server will start on `http://localhost:8085`.

## API Endpoints

### 1. Upload Avatar (Single File)

**Endpoint:** `POST /users/{userId}/avatar`

**Description:** Upload a user avatar image with validation

**Request:**
```bash
curl -X POST http://localhost:8085/users/user-123/avatar \
  -F 'avatar=@/path/to/image.jpg' \
  -F 'description=Profile picture' \
  -F 'tags=profile' \
  -F 'tags=avatar'
```

**Request Schema:**
```go
type FileUploadRequest struct {
    UserID      string   `path:"userId"`           // Path parameter
    Description string   `form:"description"`      // Form field
    Tags        []string `form:"tags"`             // Multiple values
    Avatar      []byte   `form:"avatar" format:"binary"` // File field
}
```

**Response:**
```json
{
  "fileId": "a1b2c3d4e5f6",
  "filename": "image.jpg",
  "size": 245678,
  "contentType": "image/jpeg",
  "checksum": "sha256:abc123...",
  "url": "/files/user-123/a1b2c3d4e5f6",
  "uploadedAt": "2025-10-23T10:30:00Z"
}
```

**Validations:**
- File types: JPEG, PNG, GIF, WebP only
- Maximum size: 10MB
- Magic byte verification (prevents spoofing)
- Filename sanitization

### 2. Upload Documents (Multiple Files)

**Endpoint:** `POST /users/{userId}/documents`

**Description:** Upload multiple files in a single request

**Request:**
```bash
curl -X POST http://localhost:8085/users/user-123/documents \
  -F 'documents=@/path/to/doc1.pdf' \
  -F 'documents=@/path/to/doc2.pdf' \
  -F 'documents=@/path/to/doc3.pdf' \
  -F 'folder=contracts'
```

**Response:**
```json
{
  "files": [
    {
      "fileId": "f1a2b3c4d5e6",
      "filename": "doc1.pdf",
      "size": 1245678,
      "contentType": "application/pdf",
      "checksum": "sha256:def456...",
      "url": "/files/user-123/contracts/f1a2b3c4d5e6",
      "uploadedAt": "2025-10-23T10:30:00Z"
    },
    // ... more files
  ],
  "totalSize": 5234567,
  "count": 3
}
```

**Validations:**
- Maximum total size: 500MB
- Per-file validation
- Aggregate size check

## Using Multipart Forms in Forge

### 1. Context Methods

Forge provides native multipart form methods:

```go
// Get single file
file, header, err := ctx.FormFile("avatar")
if err != nil {
    return nil, forge.BadRequest("file not found")
}
defer file.Close()

// Get multiple files with same field name
files, err := ctx.FormFiles("documents")
if err != nil {
    return nil, forge.BadRequest("no files found")
}

// Get form field value
description := ctx.FormValue("description")

// Get multiple values for same field
tags := ctx.FormValues("tags")

// Parse multipart form with custom memory limit
err := ctx.ParseMultipartForm(32 << 20) // 32MB max memory
```

### 2. Request Schema Tags

Use `form:` tags for multipart form fields:

```go
type UploadRequest struct {
    // Path parameter
    UserID string `path:"userId"`
    
    // Form fields (regular data)
    Title       string   `form:"title" description:"Document title"`
    Description string   `form:"description,omitempty"`
    Tags        []string `form:"tags,omitempty"`
    
    // File fields (binary data)
    Document []byte `form:"document" format:"binary"`
    Thumbnail []byte `form:"thumbnail,omitempty" format:"binary"`
}
```

**Tag Priority:**
1. `path:"name"` - Path parameter
2. `query:"name"` - Query parameter  
3. `header:"name"` - Header parameter
4. `form:"name"` - Multipart form field
5. `json:"name"` or `body:""` - JSON body field

### 3. OpenAPI Integration

Forge automatically generates proper OpenAPI schemas for multipart forms:

- Auto-detects `multipart/form-data` content type when `form:` tags or `format:"binary"` are present
- Generates proper encoding for binary fields
- Documents all form fields with descriptions and validations

**Generated OpenAPI:**
```yaml
/users/{userId}/avatar:
  post:
    requestBody:
      required: true
      content:
        multipart/form-data:
          schema:
            type: object
            properties:
              avatar:
                type: string
                format: binary
                description: User avatar image
              description:
                type: string
                description: File description
                maxLength: 500
              tags:
                type: array
                items:
                  type: string
            required:
              - avatar
          encoding:
            avatar:
              contentType: application/octet-stream
```

## Security Best Practices

This example implements several security measures:

### 1. File Size Limits
```go
const (
    MaxAvatarSize   = 10 * 1024 * 1024  // 10MB
    MaxDocumentSize = 100 * 1024 * 1024 // 100MB
    MaxTotalSize    = 500 * 1024 * 1024 // 500MB
)
```

### 2. Content Type Validation
```go
var allowedImageTypes = map[string]bool{
    "image/jpeg": true,
    "image/png":  true,
    "image/gif":  true,
    "image/webp": true,
}
```

### 3. Magic Byte Verification
Prevents file type spoofing by checking actual file content:
```go
func isValidImageType(data []byte, contentType string) bool {
    switch contentType {
    case "image/jpeg":
        return data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF
    case "image/png":
        return data[0] == 0x89 && data[1] == 0x50 // ...
    // ...
    }
}
```

### 4. Filename Sanitization
Removes path traversal attempts and dangerous characters:
```go
func sanitizeFilename(filename string) string {
    // Remove path components
    filename = filepath.Base(filename)
    
    // Replace dangerous characters
    filename = replacer.Replace(filename)
    
    // Limit length
    if len(filename) > 255 {
        // truncate...
    }
    
    return filename
}
```

### 5. Checksum Generation
Generate SHA-256 checksums for integrity verification:
```go
hash := sha256.Sum256(data)
checksum := hex.EncodeToString(hash[:])
```

## Production Integration

### Storage Backends

In production, integrate with storage services:

```go
// AWS S3
import "github.com/aws/aws-sdk-go/service/s3"

func (s *FileService) saveToS3(ctx context.Context, fileID string, data []byte, contentType string) error {
    _, err := s.s3Client.PutObject(&s3.PutObjectInput{
        Bucket:      aws.String("my-bucket"),
        Key:         aws.String(fileID),
        Body:        bytes.NewReader(data),
        ContentType: aws.String(contentType),
    })
    return err
}

// Azure Blob Storage
import "github.com/Azure/azure-storage-blob-go/azblob"

// Google Cloud Storage
import "cloud.google.com/go/storage"

// Local Filesystem with Forge Storage Extension
import "github.com/xraph/forge/extensions/storage"
```

### Streaming Large Files

For files > 100MB, use streaming to avoid memory issues:

```go
func (s *FileService) StreamUpload(ctx forge.Context) error {
    file, header, err := ctx.FormFile("document")
    if err != nil {
        return err
    }
    defer file.Close()

    // Stream directly to storage
    writer := s.storageBackend.OpenWriter(ctx.Context(), fileID)
    defer writer.Close()
    
    _, err = io.Copy(writer, file)
    return err
}
```

### Rate Limiting

Implement rate limiting for upload endpoints:

```go
router.POST("/users/:userId/avatar", (*FileService).UploadAvatar,
    forge.WithRateLimit(10, time.Minute), // 10 uploads per minute
    // ... other options
)
```

## Testing

Test file uploads with multipart forms:

```go
func TestFileUpload(t *testing.T) {
    // Create multipart form
    body := &bytes.Buffer{}
    writer := multipart.NewWriter(body)
    
    // Add file
    fileWriter, _ := writer.CreateFormFile("avatar", "test.jpg")
    fileWriter.Write(testImageData)
    
    // Add form fields
    writer.WriteField("description", "Test avatar")
    writer.Close()
    
    // Create request
    req := httptest.NewRequest("POST", "/users/test-user/avatar", body)
    req.Header.Set("Content-Type", writer.FormDataContentType())
    
    // ... test handler
}
```

## OpenAPI Documentation

View the generated API documentation at:
- **Swagger UI**: http://localhost:8085/docs
- **OpenAPI Spec**: http://localhost:8085/openapi.json

The documentation includes:
- All endpoints with multipart/form-data support
- Field descriptions and validation rules
- Request/response examples
- Interactive API testing

## References

- [Forge Documentation](../../_impl_docs/docs/)
- [OpenAPI Multipart Guide](../../_impl_docs/docs/openapi-guide.md)
- [Context API Reference](../../_impl_docs/docs/context-api.md)

