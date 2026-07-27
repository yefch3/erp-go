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
	Endpoint  string // "localhost:19000"
	AccessKey string
	SecretKey string
	UseSSL    bool
	Bucket    string
}

type Store struct {
	client *minio.Client
	bucket string
}

func New(ctx context.Context, cfg Config) (*Store, error) {
	c, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("blobstore: connect: %w", err)
	}
	s := &Store{client: c, bucket: cfg.Bucket}
	if err := s.ensureBucket(ctx); err != nil {
		return nil, err
	}
	return s, nil
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
	u, err := s.client.PresignedGetObject(ctx, s.bucket, key, expiry, nil)
	if err != nil {
		return nil, fmt.Errorf("blobstore: presign %s: %w", key, err)
	}
	return u, nil
}

func (s *Store) Remove(ctx context.Context, key string) error {
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("blobstore: remove %s: %w", key, err)
	}
	return nil
}
