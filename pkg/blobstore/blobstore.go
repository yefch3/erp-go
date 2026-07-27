// Package blobstore stores attachments (bank slips, LC files, documents,
// product images) behind the S3 API: MinIO locally, S3/OSS in the cloud.
// Databases store object keys ("bucket/2026/07/uuid.pdf"), never local paths.
package blobstore

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Config struct {
	Endpoint  string // reachable from this process, e.g. "minio:9000"
	AccessKey string
	SecretKey string
	UseSSL    bool
	Bucket    string
	// PublicEndpoint is where *browsers* reach the same storage, e.g.
	// "localhost:19000" while the service itself talks to "minio:9000".
	// Presigned URLs must be signed for the host that will be dialled -
	// SigV4 covers the Host header, so rewriting the URL afterwards would
	// invalidate the signature. Empty means "same as Endpoint".
	PublicEndpoint string
	// Region is pinned rather than discovered: signing a URL for the public
	// endpoint must not require a network round trip to it, which a service
	// inside Docker cannot make ("localhost" there is the container itself).
	Region string
}

type Store struct {
	client  *minio.Client
	presign *minio.Client
	bucket  string
}

func New(ctx context.Context, cfg Config) (*Store, error) {
	c, err := dial(cfg, cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	s := &Store{client: c, presign: c, bucket: cfg.Bucket}
	if cfg.PublicEndpoint != "" && cfg.PublicEndpoint != cfg.Endpoint {
		if s.presign, err = dial(cfg, cfg.PublicEndpoint); err != nil {
			return nil, err
		}
	}
	if err := s.ensureBucket(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func dial(cfg Config, endpoint string) (*minio.Client, error) {
	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}
	c, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: region,
	})
	if err != nil {
		return nil, fmt.Errorf("blobstore: connect %s: %w", endpoint, err)
	}
	return c, nil
}

func (s *Store) ensureBucket(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("blobstore: bucket check: %w", err)
	}
	if !exists {
		if err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{}); err != nil {
			return fmt.Errorf("blobstore: create bucket: %w", err)
		}
	}
	return nil
}

// Put streams an object and returns the key to persist in the database.
func (s *Store) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) (string, error) {
	_, err := s.client.PutObject(ctx, s.bucket, key, r, size,
		minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return "", fmt.Errorf("blobstore: put %s: %w", key, err)
	}
	return key, nil
}

func (s *Store) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("blobstore: get %s: %w", key, err)
	}
	return obj, nil
}

// PresignedGet hands the browser a time-limited download URL so file bytes
// never stream through the gateway.
func (s *Store) PresignedGet(ctx context.Context, key string, expiry time.Duration) (*url.URL, error) {
	u, err := s.presign.PresignedGetObject(ctx, s.bucket, key, expiry, nil)
	if err != nil {
		return nil, fmt.Errorf("blobstore: presign %s: %w", key, err)
	}
	return u, nil
}

// PresignedPut is the upload counterpart: the browser PUTs the bytes to this
// URL directly, so a 50 MB scan never occupies gateway memory or bandwidth.
// The caller decides the key, which is what it later stores in its own table.
func (s *Store) PresignedPut(ctx context.Context, key string, expiry time.Duration) (*url.URL, error) {
	u, err := s.presign.PresignedPutObject(ctx, s.bucket, key, expiry)
	if err != nil {
		return nil, fmt.Errorf("blobstore: presign put %s: %w", key, err)
	}
	return u, nil
}

func (s *Store) Remove(ctx context.Context, key string) error {
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("blobstore: remove %s: %w", key, err)
	}
	return nil
}
