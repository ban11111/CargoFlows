package app

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"cargoflow/api/internal/config"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const publicReadPolicy = `{
  "Version":"2012-10-17",
  "Statement":[{
    "Effect":"Allow",
    "Principal":{"AWS":["*"]},
    "Action":["s3:GetObject"],
    "Resource":["arn:aws:s3:::%s/*"]
  }]
}`

type objectStore struct {
	internal  *minio.Client
	public    *minio.Client
	bucket    string
	publicURL *url.URL
}

func newObjectStore(cfg config.Config) (*objectStore, error) {
	options := &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinIOAccessKey, cfg.MinIOSecretKey, ""),
		Secure: false,
		Region: "us-east-1",
	}
	internal, err := minio.New(cfg.MinIOEndpoint, options)
	if err != nil {
		return nil, err
	}
	public, err := minio.New(cfg.MinIOPublicEndpoint, options)
	if err != nil {
		return nil, err
	}
	publicURL, err := url.Parse("http://" + cfg.MinIOPublicEndpoint)
	if err != nil {
		return nil, err
	}
	return &objectStore{internal: internal, public: public, bucket: cfg.MinIOBucket, publicURL: publicURL}, nil
}

func (s *objectStore) createUploadURL(ctx context.Context, objectKey string) (string, string, error) {
	if err := s.ensureBucket(ctx); err != nil {
		return "", "", err
	}

	uploadURL, err := s.public.PresignedPutObject(ctx, s.bucket, objectKey, 15*time.Minute)
	if err != nil {
		return "", "", err
	}
	assetURL := *s.publicURL
	assetURL.Path = "/" + s.bucket + "/" + objectKey
	return uploadURL.String(), assetURL.String(), nil
}

func (s *objectStore) ensureBucket(ctx context.Context) error {
	exists, err := s.internal.BucketExists(ctx, s.bucket)
	if err != nil {
		return err
	}
	if !exists {
		if err := s.internal.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{Region: "us-east-1"}); err != nil {
			return err
		}
	}
	return s.internal.SetBucketPolicy(ctx, s.bucket, fmt.Sprintf(publicReadPolicy, s.bucket))
}
