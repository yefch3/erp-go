// Package blobstore stores attachments (bank slips, LC files, documents,
// product images) behind the S3 API: MinIO locally, S3/OSS in the cloud.
// Databases store object keys ("bucket/2026/07/uuid.pdf"), never local paths.
package blobstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
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
	s, err := NewOffline(cfg)
	if err != nil {
		return nil, err
	}
	if err := s.ensureBucket(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

// NewOffline is New without the bucket check, so it makes no network call.
//
// It is for code that only signs, and for tests that have to sign for a store
// they cannot reach — production's included: what a presigned URL looks like
// is decided entirely by the configuration, and a test that wants to know
// should ask the signer rather than write the answer down (see URLShape).
// Reads and writes through it work only if the bucket happens to exist.
func NewOffline(cfg Config) (*Store, error) {
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

// URLShape is where the presigned URLs of a Config point: the scheme, the
// host, and the part of the path that comes before the object key.
//
// It exists because the answer cannot be written down by hand. For an Amazon
// endpoint the client does not sign for the endpoint it was given; it signs
// for a per-region, dual-stack, virtual-hosted name derived from it —
// "s3.us-west-2.amazonaws.com" becomes
// "<bucket>.s3.dualstack.us-west-2.amazonaws.com" — and which name that is
// depends on the region, on how the bucket is spelt and on the version of the
// library. The gateway's file relay once carried its own idea of that name.
// It was the obvious one, it was wrong, and for two days in production it
// turned away every upload it was built to carry (2026-11-09 to 11-11) while
// every test, written against the same idea, stayed green.
//
// So whoever has to recognise a presigned URL asks here, and is answered by
// the client that signs them.
type URLShape struct {
	Scheme string // "http" or "https"
	Host   string // as signed, port included when the endpoint has one
	// PathPrefix is "/" for a virtual-hosted bucket (the bucket is in the
	// host) and "/<bucket>/" for path-style (MinIO, and most non-Amazon
	// stores).
	PathPrefix string
}

// Covers reports whether u addresses an object of this store: same scheme,
// same host, and a key after the prefix. Nothing but the host's letter case
// is normalised, because the signature was computed over the URL as written.
func (s URLShape) Covers(u *url.URL) bool {
	return u != nil &&
		u.Scheme == s.Scheme &&
		s.SameHost(u.Host) &&
		strings.HasPrefix(u.Path, s.PathPrefix) &&
		len(u.Path) > len(s.PathPrefix)
}

// SameHost reports whether host is this store's host, ignoring ASCII letter
// case and nothing else.
//
// Not strings.EqualFold: that folds Unicode, under which the Kelvin sign
// (U+212A) equals "k" — a host that is not ours, written to compare as if it
// were. Where such a name would resolve is beside the point; a check whose
// job is to recognise one exact host should not have an opinion about
// alphabets.
func (s URLShape) SameHost(host string) bool {
	if s.Host == "" || len(host) != len(s.Host) {
		return false
	}
	for i := 0; i < len(host); i++ {
		a, b := host[i], s.Host[i]
		if a >= utf8RuneSelf || b >= utf8RuneSelf {
			return false
		}
		if 'A' <= a && a <= 'Z' {
			a += 'a' - 'A'
		}
		if 'A' <= b && b <= 'Z' {
			b += 'a' - 'A'
		}
		if a != b {
			return false
		}
	}
	return true
}

// utf8RuneSelf is utf8.RuneSelf: bytes below it are ASCII.
const utf8RuneSelf = 0x80

// shapeProbeKey is the object name signed to learn a shape. It is never
// stored and never sent anywhere.
const shapeProbeKey = "shape-probe"

// PresignedShapes reports the shape of the URLs cfg signs for browsers
// (public: PublicEndpoint, or Endpoint when that is empty) and the shape the
// same objects have when addressed through Endpoint from inside this process
// (internal). The two are identical wherever the two endpoints are.
//
// It needs neither the network nor real credentials: a throwaway name is
// signed with placeholder keys and only its address is kept. cfg must carry
// the same Endpoint, PublicEndpoint, UseSSL, Bucket and Region as the Config
// the signing service runs with — the region most of all, since on Amazon it
// is part of the host.
func PresignedShapes(cfg Config) (public, internal URLShape, err error) {
	if internal, err = shapeOf(cfg, cfg.Endpoint); err != nil {
		return URLShape{}, URLShape{}, err
	}
	if cfg.PublicEndpoint == "" || cfg.PublicEndpoint == cfg.Endpoint {
		return internal, internal, nil
	}
	if public, err = shapeOf(cfg, cfg.PublicEndpoint); err != nil {
		return URLShape{}, URLShape{}, err
	}
	return public, internal, nil
}

func shapeOf(cfg Config, endpoint string) (URLShape, error) {
	// The signature is thrown away, so the keys only have to exist: the
	// client refuses to presign anonymously.
	cfg.AccessKey, cfg.SecretKey = shapeProbeKey, shapeProbeKey
	c, err := dial(cfg, endpoint)
	if err != nil {
		return URLShape{}, err
	}
	u, err := c.PresignedPutObject(context.Background(), cfg.Bucket, shapeProbeKey, time.Minute)
	if err != nil {
		return URLShape{}, fmt.Errorf("blobstore: learn the URL shape of %s: %w", endpoint, err)
	}
	prefix, ok := strings.CutSuffix(u.Path, shapeProbeKey)
	if !ok || !strings.HasPrefix(prefix, "/") || u.Host == "" {
		return URLShape{}, fmt.Errorf("blobstore: learn the URL shape of %s: unexpected address %s://%s%s",
			endpoint, u.Scheme, u.Host, u.Path)
	}
	return URLShape{Scheme: u.Scheme, Host: u.Host, PathPrefix: prefix}, nil
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

// PresignedGetAs is PresignedGet with the name the file should be saved under.
//
// Object keys are random by design — "mail-attachments/1/9f3c….pptx" — so a
// plain download URL saves the customer's quotation as a hex string. The name
// travels as a response-content-disposition override, which the store signs
// along with everything else: the browser cannot be told to rename it, and a
// tampered name invalidates the signature.
func (s *Store) PresignedGetAs(ctx context.Context, key, saveAs string, expiry time.Duration) (*url.URL, error) {
	params := url.Values{}
	if saveAs != "" {
		// RFC 6266: the ASCII form for old clients, filename* for everything
		// written after 2011 — without the latter a Chinese file name arrives
		// as mojibake or gets dropped.
		params.Set("response-content-disposition",
			`attachment; filename="`+asciiFallback(saveAs)+`"; filename*=UTF-8''`+url.PathEscape(saveAs))
	}
	u, err := s.presign.PresignedGetObject(ctx, s.bucket, key, expiry, params)
	if err != nil {
		return nil, fmt.Errorf("blobstore: presign %s: %w", key, err)
	}
	return u, nil
}

// asciiFallback keeps the quoted filename parameter legal for clients that
// only read that one. Anything outside printable ASCII, plus the quote and
// backslash that would end the parameter early, becomes an underscore.
func asciiFallback(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			b.WriteByte('_')
			continue
		}
		b.WriteRune(r)
	}
	if b.Len() == 0 {
		return "download"
	}
	return b.String()
}

// PresignedGetInline is the preview counterpart: the same signed read, asking
// the browser to render the object rather than save it.
//
// The content type is pinned by the caller instead of trusting what the store
// happens to have recorded. An object whose stored type says text/html would
// otherwise render as a page on the storage origin, and "preview this
// attachment" would become "run this sender's markup" — hence callers pass
// only types they have decided are safe to display.
func (s *Store) PresignedGetInline(ctx context.Context, key, contentType string, expiry time.Duration) (*url.URL, error) {
	params := url.Values{}
	params.Set("response-content-disposition", "inline")
	if contentType != "" {
		params.Set("response-content-type", contentType)
	}
	u, err := s.presign.PresignedGetObject(ctx, s.bucket, key, expiry, params)
	if err != nil {
		return nil, fmt.Errorf("blobstore: presign inline %s: %w", key, err)
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

// Stat reports what is actually in the bucket under a key.
//
// Needed whenever an upload went straight to storage through a presigned URL:
// the size the client reports afterwards is a claim, not a fact, so any cap
// enforced on it is advisory. Reading the object's real size closes that.
// ErrNotFound 是 Stat 对「没有这个键」的回答，和「存储不通」分开：有人要靠
// 这个区别决定接下来做不做一件花钱的事（mail 服务恢复转换结果时，没有就
// 重跑模型，不通就等）。Get 给不了这个区别——对象是懒取的，键不存在要到
// 第一次 Read 才知道。
var ErrNotFound = errors.New("blobstore: object not found")

func (s *Store) Stat(ctx context.Context, key string) (int64, string, error) {
	info, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		if resp := minio.ToErrorResponse(err); resp.Code == "NoSuchKey" || resp.StatusCode == 404 {
			return 0, "", fmt.Errorf("blobstore: stat %s: %w", key, ErrNotFound)
		}
		return 0, "", fmt.Errorf("blobstore: stat %s: %w", key, err)
	}
	return info.Size, info.ContentType, nil
}
