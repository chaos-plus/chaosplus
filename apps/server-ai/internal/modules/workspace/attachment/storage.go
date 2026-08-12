package attachment

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"

	"github.com/chaos-plus/chaosplus/internal/core/extension/secretx"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type StorageConfig struct {
	Endpoint      string `mapstructure:"endpoint" description:"S3-compatible endpoint with optional http or https scheme"`
	Bucket        string `mapstructure:"bucket" description:"pre-provisioned attachment bucket"`
	Region        string `mapstructure:"region" description:"S3 region" default:""`
	AccessKey     string `mapstructure:"access_key" description:"S3 access key; prefer access_key_file" default:""`
	AccessKeyFile string `mapstructure:"access_key_file" description:"file containing the S3 access key" default:""`
	SecretKey     string `mapstructure:"secret_key" description:"S3 secret key; prefer secret_key_file" default:""`
	SecretKeyFile string `mapstructure:"secret_key_file" description:"file containing the S3 secret key" default:""`
}

type S3BlobStore struct {
	client    *minio.Client
	bucket    string
	region    string
	checkOnce sync.Once
	checkErr  error
}

func NewS3BlobStore(config StorageConfig) (*S3BlobStore, error) {
	accessKey, err := secretx.Resolve("storage.access_key", config.AccessKey, config.AccessKeyFile, 64<<10)
	if err != nil {
		return nil, err
	}
	secretKey, err := secretx.Resolve("storage.secret_key", config.SecretKey, config.SecretKeyFile, 64<<10)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(config.Endpoint) == "" || strings.TrimSpace(config.Bucket) == "" ||
		strings.TrimSpace(accessKey) == "" || strings.TrimSpace(secretKey) == "" {
		return nil, fmt.Errorf("attachment storage requires endpoint, bucket, access key, and secret key")
	}
	address, secure, err := storageEndpoint(config.Endpoint)
	if err != nil {
		return nil, err
	}
	client, err := minio.New(address, &minio.Options{Creds: credentials.NewStaticV4(accessKey, secretKey, ""), Secure: secure, Region: config.Region})
	if err != nil {
		return nil, fmt.Errorf("build attachment storage client: %w", err)
	}
	return &S3BlobStore{client: client, bucket: config.Bucket, region: config.Region}, nil
}

func storageEndpoint(endpoint string) (string, bool, error) {
	endpoint = strings.TrimSpace(endpoint)
	if !strings.Contains(endpoint, "://") {
		return endpoint, false, nil
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Path != "" {
		return "", false, fmt.Errorf("invalid attachment storage endpoint")
	}
	return parsed.Host, parsed.Scheme == "https", nil
}

func (s *S3BlobStore) ready(ctx context.Context) error {
	s.checkOnce.Do(func() {
		exists, err := s.client.BucketExists(ctx, s.bucket)
		if err != nil {
			s.checkErr = fmt.Errorf("check attachment bucket: %w", err)
			return
		}
		if !exists {
			s.checkErr = fmt.Errorf("attachment bucket is not provisioned")
		}
	})
	return s.checkErr
}

func (s *S3BlobStore) Put(ctx context.Context, key string, content io.Reader, size int64, contentType string) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	_, err := s.client.PutObject(ctx, s.bucket, key, content, size, minio.PutObjectOptions{ContentType: contentType})
	return err
}

func (s *S3BlobStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	if _, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{}); err != nil {
		return nil, err
	}
	return s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
}

func (s *S3BlobStore) Delete(ctx context.Context, key string) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	return s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}
