package s3

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/watchword/watchword/internal/config"
)

// Presigner generates presigned URLs for S3 PUT and GET operations,
// and supports direct object deletion for cleanup.
type Presigner interface {
	PresignPUT(ctx context.Context, key string, contentType string, maxSize int64) (string, error)
	PresignGET(ctx context.Context, key string, downloadFilename string) (string, error)
	DeleteObject(ctx context.Context, key string) error
}

type Client struct {
	s3Client      *s3.Client
	presignClient *s3.PresignClient
	bucket        string
	presignTTL    time.Duration
}

func NewClient(ctx context.Context, cfg *config.S3Config) (*Client, error) {
	accessKey := os.Getenv("WORDSTORE_S3_ACCESS_KEY_ID")
	secretKey := os.Getenv("WORDSTORE_S3_SECRET_ACCESS_KEY")
	if accessKey == "" || secretKey == "" {
		return nil, fmt.Errorf("WORDSTORE_S3_ACCESS_KEY_ID and WORDSTORE_S3_SECRET_ACCESS_KEY must be set")
	}

	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		),
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}

	var s3Opts []func(*s3.Options)
	if cfg.Endpoint != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = true // required for R2 and MinIO
		})
	}

	s3Client := s3.NewFromConfig(awsCfg, s3Opts...)
	presignClient := s3.NewPresignClient(s3Client)

	return &Client{
		s3Client:      s3Client,
		presignClient: presignClient,
		bucket:        cfg.Bucket,
		presignTTL:    time.Duration(cfg.PresignTTLMinutes) * time.Minute,
	}, nil
}

func (c *Client) DeleteObject(ctx context.Context, key string) error {
	_, err := c.s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("deleting S3 object %s: %w", key, err)
	}
	return nil
}

func (c *Client) PresignPUT(ctx context.Context, key string, contentType string, maxSize int64) (string, error) {
	input := &s3.PutObjectInput{
		Bucket:        aws.String(c.bucket),
		Key:           aws.String(key),
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(maxSize),
	}

	result, err := c.presignClient.PresignPutObject(ctx, input,
		s3.WithPresignExpires(c.presignTTL),
	)
	if err != nil {
		return "", fmt.Errorf("presigning PUT: %w", err)
	}
	return result.URL, nil
}

func (c *Client) PresignGET(ctx context.Context, key string, downloadFilename string) (string, error) {
	input := &s3.GetObjectInput{
		Bucket:                     aws.String(c.bucket),
		Key:                        aws.String(key),
		ResponseContentDisposition: aws.String(fmt.Sprintf(`attachment; filename="%s"`, downloadFilename)),
	}

	result, err := c.presignClient.PresignGetObject(ctx, input,
		s3.WithPresignExpires(c.presignTTL),
	)
	if err != nil {
		return "", fmt.Errorf("presigning GET: %w", err)
	}
	return result.URL, nil
}
