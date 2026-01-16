package storage

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/go-utils/metrics"
)

// S3Backend implements storage using AWS S3.
type S3Backend struct {
	client     *s3.Client
	uploader   *manager.Uploader
	downloader *manager.Downloader
	bucket     string
	region     string
	prefix     string
	logger     forge.Logger
	metrics    forge.Metrics
	validator  *PathValidator
}

// S3Config contains S3 configuration.
type S3Config struct {
	Region          string
	Bucket          string
	Prefix          string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	Endpoint        string // For S3-compatible services
	UsePathStyle    bool   // For S3-compatible services
}

// NewS3Backend creates a new S3 storage backend.
func NewS3Backend(configMap map[string]any, logger forge.Logger, metrics forge.Metrics) (*S3Backend, error) {
	// Parse configuration
	s3Config := parseS3Config(configMap)

	// Validate required fields
	if s3Config.Bucket == "" {
		return nil, errors.New("S3 bucket is required")
	}

	if s3Config.Region == "" {
		s3Config.Region = "us-east-1" // Default region
	}

	// Build AWS config
	var opts []func(*config.LoadOptions) error

	opts = append(opts, config.WithRegion(s3Config.Region))

	// Add credentials if provided
	if s3Config.AccessKeyID != "" && s3Config.SecretAccessKey != "" {
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				s3Config.AccessKeyID,
				s3Config.SecretAccessKey,
				s3Config.SessionToken,
			),
		))
	}

	// Load AWS config
	cfg, err := config.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create S3 client with custom options
	var clientOpts []func(*s3.Options)
	if s3Config.Endpoint != "" {
		clientOpts = append(clientOpts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(s3Config.Endpoint)
			o.UsePathStyle = s3Config.UsePathStyle
		})
	}

	client := s3.NewFromConfig(cfg, clientOpts...)

	// Create uploader and downloader
	uploader := manager.NewUploader(client, func(u *manager.Uploader) {
		u.PartSize = 10 * 1024 * 1024 // 10 MB per part
		u.Concurrency = 5             // 5 concurrent uploads
	})

	downloader := manager.NewDownloader(client, func(d *manager.Downloader) {
		d.PartSize = 10 * 1024 * 1024 // 10 MB per part
		d.Concurrency = 5             // 5 concurrent downloads
	})

	backend := &S3Backend{
		client:     client,
		uploader:   uploader,
		downloader: downloader,
		bucket:     s3Config.Bucket,
		region:     s3Config.Region,
		prefix:     s3Config.Prefix,
		logger:     logger,
		metrics:    metrics,
		validator:  NewPathValidator(),
	}

	logger.Info("S3 backend initialized",
		forge.F("bucket", s3Config.Bucket),
		forge.F("region", s3Config.Region),
		forge.F("prefix", s3Config.Prefix),
	)

	return backend, nil
}

// Upload uploads a file to S3.
func (b *S3Backend) Upload(ctx context.Context, key string, data io.Reader, opts ...UploadOption) error {
	start := time.Now()

	// Validate key
	if err := b.validator.ValidateKey(key); err != nil {
		return fmt.Errorf("invalid key: %w", err)
	}

	options := applyUploadOptions(opts...)

	// Add prefix if configured
	s3Key := b.getS3Key(key)

	// Prepare upload input
	input := &s3.PutObjectInput{
		Bucket:      aws.String(b.bucket),
		Key:         aws.String(s3Key),
		Body:        data,
		ContentType: aws.String(options.ContentType),
	}

	// Add metadata
	if len(options.Metadata) > 0 {
		input.Metadata = options.Metadata
	}

	// Set ACL if provided
	if options.ACL != "" {
		input.ACL = types.ObjectCannedACL(options.ACL)
	}

	// Upload using uploader for large files support
	result, err := b.uploader.Upload(ctx, input)
	if err != nil {
		b.metrics.Counter("storage_upload_errors", metrics.WithLabel("backend", "s3")).Inc()

		return fmt.Errorf("failed to upload to S3: %w", err)
	}

	duration := time.Since(start)
	b.metrics.Histogram("storage_upload_duration", metrics.WithLabel("backend", "s3")).Observe(duration.Seconds())
	b.metrics.Counter("storage_uploads", metrics.WithLabel("backend", "s3")).Inc()

	b.logger.Debug("file uploaded to S3",
		forge.F("key", key),
		forge.F("s3_key", s3Key),
		forge.F("location", result.Location),
		forge.F("duration", duration),
	)

	return nil
}

// Download downloads a file from S3.
func (b *S3Backend) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	// Validate key
	if err := b.validator.ValidateKey(key); err != nil {
		return nil, fmt.Errorf("invalid key: %w", err)
	}

	s3Key := b.getS3Key(key)

	// Get object
	result, err := b.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(s3Key),
	})
	if err != nil {
		if isS3NotFoundError(err) {
			return nil, ErrObjectNotFound
		}

		b.metrics.Counter("storage_download_errors", metrics.WithLabel("backend", "s3")).Inc()

		return nil, fmt.Errorf("failed to download from S3: %w", err)
	}

	b.metrics.Counter("storage_downloads", metrics.WithLabel("backend", "s3")).Inc()

	return result.Body, nil
}

// Delete deletes a file from S3.
func (b *S3Backend) Delete(ctx context.Context, key string) error {
	// Validate key
	if err := b.validator.ValidateKey(key); err != nil {
		return fmt.Errorf("invalid key: %w", err)
	}

	s3Key := b.getS3Key(key)

	_, err := b.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(s3Key),
	})
	if err != nil {
		b.metrics.Counter("storage_delete_errors", metrics.WithLabel("backend", "s3")).Inc()

		return fmt.Errorf("failed to delete from S3: %w", err)
	}

	b.metrics.Counter("storage_deletes", metrics.WithLabel("backend", "s3")).Inc()

	return nil
}

// List lists files with a prefix.
func (b *S3Backend) List(ctx context.Context, prefix string, opts ...ListOption) ([]Object, error) {
	options := applyListOptions(opts...)

	// Validate prefix if provided
	if prefix != "" {
		cleaned := b.validator.SanitizeKey(prefix)
		if cleaned != prefix {
			prefix = cleaned
		}
	}

	s3Prefix := b.getS3Key(prefix)

	// Build list input
	input := &s3.ListObjectsV2Input{
		Bucket:  aws.String(b.bucket),
		Prefix:  aws.String(s3Prefix),
		MaxKeys: aws.Int32(1000),
	}

	if options.Marker != "" {
		input.ContinuationToken = aws.String(options.Marker)
	}

	if !options.Recursive {
		input.Delimiter = aws.String("/")
	}

	var objects []Object

	paginator := s3.NewListObjectsV2Paginator(b.client, input)

	for paginator.HasMorePages() {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list S3 objects: %w", err)
		}

		for _, obj := range page.Contents {
			// Remove prefix from key
			key := b.removePrefix(*obj.Key)

			objects = append(objects, Object{
				Key:          key,
				Size:         *obj.Size,
				LastModified: *obj.LastModified,
				ETag:         strings.Trim(*obj.ETag, "\""),
				ContentType:  "", // Not available in list
			})

			// Check limit
			if options.Limit > 0 && len(objects) >= options.Limit {
				return objects, nil
			}
		}

		// Check limit
		if options.Limit > 0 && len(objects) >= options.Limit {
			break
		}
	}

	return objects, nil
}

// Metadata retrieves object metadata.
func (b *S3Backend) Metadata(ctx context.Context, key string) (*ObjectMetadata, error) {
	// Validate key
	if err := b.validator.ValidateKey(key); err != nil {
		return nil, fmt.Errorf("invalid key: %w", err)
	}

	s3Key := b.getS3Key(key)

	result, err := b.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(s3Key),
	})
	if err != nil {
		if isS3NotFoundError(err) {
			return nil, ErrObjectNotFound
		}

		return nil, fmt.Errorf("failed to get metadata from S3: %w", err)
	}

	metadata := &ObjectMetadata{
		Key:          key,
		Size:         *result.ContentLength,
		LastModified: *result.LastModified,
		ETag:         strings.Trim(*result.ETag, "\""),
		ContentType:  aws.ToString(result.ContentType),
		Metadata:     result.Metadata,
	}

	return metadata, nil
}

// Exists checks if an object exists.
func (b *S3Backend) Exists(ctx context.Context, key string) (bool, error) {
	// Validate key
	if err := b.validator.ValidateKey(key); err != nil {
		return false, fmt.Errorf("invalid key: %w", err)
	}

	s3Key := b.getS3Key(key)

	_, err := b.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(s3Key),
	})
	if err != nil {
		if isS3NotFoundError(err) {
			return false, nil
		}

		return false, fmt.Errorf("failed to check existence in S3: %w", err)
	}

	return true, nil
}

// Copy copies a file within S3.
func (b *S3Backend) Copy(ctx context.Context, srcKey, dstKey string) error {
	// Validate keys
	if err := b.validator.ValidateKey(srcKey); err != nil {
		return fmt.Errorf("invalid source key: %w", err)
	}

	if err := b.validator.ValidateKey(dstKey); err != nil {
		return fmt.Errorf("invalid destination key: %w", err)
	}

	srcS3Key := b.getS3Key(srcKey)
	dstS3Key := b.getS3Key(dstKey)

	copySource := fmt.Sprintf("%s/%s", b.bucket, srcS3Key)

	_, err := b.client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(b.bucket),
		CopySource: aws.String(copySource),
		Key:        aws.String(dstS3Key),
	})
	if err != nil {
		if isS3NotFoundError(err) {
			return ErrObjectNotFound
		}

		return fmt.Errorf("failed to copy in S3: %w", err)
	}

	return nil
}

// Move moves a file within S3 (copy + delete).
func (b *S3Backend) Move(ctx context.Context, srcKey, dstKey string) error {
	// Copy first
	if err := b.Copy(ctx, srcKey, dstKey); err != nil {
		return err
	}

	// Delete source
	if err := b.Delete(ctx, srcKey); err != nil {
		b.logger.Warn("failed to delete source after move",
			forge.F("src_key", srcKey),
			forge.F("dst_key", dstKey),
			forge.F("error", err.Error()),
		)

		return fmt.Errorf("failed to delete source after move: %w", err)
	}

	return nil
}

// PresignUpload generates a presigned URL for upload.
func (b *S3Backend) PresignUpload(ctx context.Context, key string, expiry time.Duration) (string, error) {
	// Validate key
	if err := b.validator.ValidateKey(key); err != nil {
		return "", fmt.Errorf("invalid key: %w", err)
	}

	s3Key := b.getS3Key(key)

	presignClient := s3.NewPresignClient(b.client)

	result, err := presignClient.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(s3Key),
	}, func(opts *s3.PresignOptions) {
		opts.Expires = expiry
	})
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned upload URL: %w", err)
	}

	b.metrics.Counter("storage_presigned_urls", metrics.WithLabel("backend", "s3"), metrics.WithLabel("type", "upload")).Inc()

	return result.URL, nil
}

// PresignDownload generates a presigned URL for download.
func (b *S3Backend) PresignDownload(ctx context.Context, key string, expiry time.Duration) (string, error) {
	// Validate key
	if err := b.validator.ValidateKey(key); err != nil {
		return "", fmt.Errorf("invalid key: %w", err)
	}

	s3Key := b.getS3Key(key)

	presignClient := s3.NewPresignClient(b.client)

	result, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(s3Key),
	}, func(opts *s3.PresignOptions) {
		opts.Expires = expiry
	})
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned download URL: %w", err)
	}

	b.metrics.Counter("storage_presigned_urls", metrics.WithLabel("backend", "s3"), metrics.WithLabel("type", "download")).Inc()

	return result.URL, nil
}

// Helper: Get S3 key with prefix.
func (b *S3Backend) getS3Key(key string) string {
	if b.prefix == "" {
		return key
	}

	return b.prefix + "/" + key
}

// Helper: Remove prefix from S3 key.
func (b *S3Backend) removePrefix(s3Key string) string {
	if b.prefix == "" {
		return s3Key
	}

	prefix := b.prefix + "/"
	if after, ok := strings.CutPrefix(s3Key, prefix); ok {
		return after
	}

	return s3Key
}

// Helper: Check if error is S3 not found error.
func isS3NotFoundError(err error) bool {
	if err == nil {
		return false
	}

	// Check for NoSuchKey error
	errMsg := err.Error()

	return strings.Contains(errMsg, "NoSuchKey") ||
		strings.Contains(errMsg, "NotFound") ||
		strings.Contains(errMsg, "404")
}

// Helper: Parse S3 config from map.
func parseS3Config(configMap map[string]any) S3Config {
	config := S3Config{
		Region: "us-east-1", // Default
	}

	if v, ok := configMap["region"].(string); ok {
		config.Region = v
	}

	if v, ok := configMap["bucket"].(string); ok {
		config.Bucket = v
	}

	if v, ok := configMap["prefix"].(string); ok {
		config.Prefix = v
	}

	if v, ok := configMap["access_key_id"].(string); ok {
		config.AccessKeyID = v
	}

	if v, ok := configMap["secret_access_key"].(string); ok {
		config.SecretAccessKey = v
	}

	if v, ok := configMap["session_token"].(string); ok {
		config.SessionToken = v
	}

	if v, ok := configMap["endpoint"].(string); ok {
		config.Endpoint = v
	}

	if v, ok := configMap["use_path_style"].(bool); ok {
		config.UsePathStyle = v
	}

	return config
}
