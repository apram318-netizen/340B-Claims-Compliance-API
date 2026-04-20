package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Storage stores export files in Amazon S3 and serves downloads via
// short-lived presigned URLs, keeping the application stateless.
//
// Required IAM permissions: s3:PutObject, s3:GetObject on the configured bucket.
// Configure credentials via the standard AWS credential chain
// (IAM role, env vars AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY, ~/.aws/credentials).
type S3Storage struct {
	client *s3.Client
	bucket string
	prefix string
}

// NewS3Storage creates an S3Storage using the default AWS credential chain.
// prefix is prepended to every object key (e.g. "exports/").
func NewS3Storage(ctx context.Context, bucket, prefix string) (*S3Storage, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}
	return &S3Storage{
		client: s3.NewFromConfig(cfg),
		bucket: bucket,
		prefix: prefix,
	}, nil
}

// Create opens a temporary local file for streaming writes.
// Commit uploads the temp file to S3 and returns the S3 object key.
func (s *S3Storage) Create(name string) (ExportWriter, error) {
	tmp, err := os.CreateTemp("", "export-*.csv")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	return &s3Writer{
		tmp:    tmp,
		client: s.client,
		bucket: s.bucket,
		key:    s.prefix + name,
	}, nil
}

// ServeDownload generates a 15-minute presigned GET URL and redirects the
// client. The redirect is a 307 so the browser retains the download intent.
func (s *S3Storage) ServeDownload(ctx context.Context, ref, filename string, w http.ResponseWriter, r *http.Request) error {
	presigner := s3.NewPresignClient(s.client)
	req, err := presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket:                     aws.String(s.bucket),
		Key:                        aws.String(ref),
		ResponseContentDisposition: aws.String(`attachment; filename="` + filename + `"`),
	}, s3.WithPresignExpires(15*time.Minute))
	if err != nil {
		return fmt.Errorf("presign S3 URL: %w", err)
	}
	http.Redirect(w, r, req.URL, http.StatusTemporaryRedirect)
	return nil
}

type s3Writer struct {
	tmp    *os.File
	client *s3.Client
	bucket string
	key    string
}

func (sw *s3Writer) Write(p []byte) (int, error) { return sw.tmp.Write(p) }

func (sw *s3Writer) Commit() (string, error) {
	if _, err := sw.tmp.Seek(0, io.SeekStart); err != nil {
		sw.cleanup()
		return "", err
	}
	stat, err := sw.tmp.Stat()
	if err != nil {
		sw.cleanup()
		return "", err
	}

	_, err = sw.client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket:        aws.String(sw.bucket),
		Key:           aws.String(sw.key),
		Body:          sw.tmp,
		ContentType:   aws.String("text/csv"),
		ContentLength: aws.Int64(stat.Size()),
	})
	sw.cleanup()
	if err != nil {
		return "", fmt.Errorf("upload to S3 (key=%s): %w", sw.key, err)
	}
	return sw.key, nil
}

func (sw *s3Writer) Abort() { sw.cleanup() }

func (sw *s3Writer) cleanup() {
	sw.tmp.Close()
	os.Remove(sw.tmp.Name())
}
