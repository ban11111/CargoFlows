package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"cargoflow/api/internal/ai"
	"cargoflow/api/internal/config"
	"github.com/google/uuid"
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

const generatedBucketPolicy = ""

type objectStore struct {
	internal  *minio.Client
	public    *minio.Client
	bucket    string
	aiBucket  string
	publicURL *url.URL
}

type assetStorage interface {
	createUploadURL(context.Context, string) (string, string, error)
	assetURL(string) string
	objectExists(context.Context, string) (bool, error)
}

func newObjectStore(cfg config.Config) (*objectStore, error) {
	if strings.TrimSpace(cfg.MinIOBucket) == "" || strings.TrimSpace(cfg.MinIOAIBucket) == "" || cfg.MinIOBucket == cfg.MinIOAIBucket {
		return nil, fmt.Errorf("source and generated MinIO buckets must be distinct and non-empty")
	}
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
	return &objectStore{internal: internal, public: public, bucket: cfg.MinIOBucket, aiBucket: cfg.MinIOAIBucket, publicURL: publicURL}, nil
}

func NewImageObjectStore(cfg config.Config) (ai.ImageObjectStore, error) {
	return newObjectStore(cfg)
}

func (s *objectStore) createUploadURL(ctx context.Context, objectKey string) (string, string, error) {
	if err := s.ensureBucket(ctx); err != nil {
		return "", "", err
	}

	uploadURL, err := s.public.PresignedPutObject(ctx, s.bucket, objectKey, 15*time.Minute)
	if err != nil {
		return "", "", err
	}
	return uploadURL.String(), s.assetURL(objectKey), nil
}

func (s *objectStore) assetURL(objectKey string) string {
	assetURL := *s.publicURL
	assetURL.Path = "/" + s.bucket + "/" + objectKey
	return assetURL.String()
}

func (s *objectStore) objectExists(ctx context.Context, objectKey string) (bool, error) {
	_, err := s.internal.StatObject(ctx, s.bucket, objectKey, minio.StatObjectOptions{})
	if err == nil {
		return true, nil
	}
	response := minio.ToErrorResponse(err)
	if response.Code == "NoSuchKey" || response.Code == "NoSuchObject" || response.StatusCode == 404 {
		return false, nil
	}
	return false, err
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

func (s *objectStore) ReadSource(ctx context.Context, objectKey string) (ai.ImageInput, error) {
	object, err := s.internal.GetObject(ctx, s.bucket, objectKey, minio.GetObjectOptions{})
	if err != nil {
		return ai.ImageInput{}, err
	}
	defer object.Close()
	value, err := io.ReadAll(io.LimitReader(object, 50<<20+1))
	if err != nil {
		return ai.ImageInput{}, err
	}
	if len(value) > 50<<20 {
		return ai.ImageInput{}, fmt.Errorf("source image exceeds byte limit")
	}
	return ai.ImageInput{Bytes: value}, nil
}

func (s *objectStore) ClaimGenerated(ctx context.Context, objectKey, mimeType string, value []byte) (ai.GeneratedObjectClaim, error) {
	if err := s.ensureGeneratedBucket(ctx); err != nil {
		return ai.GeneratedObjectClaim{}, err
	}
	claim := ai.GeneratedObjectClaim{ObjectKey: objectKey, Token: uuid.NewString(), State: ai.GeneratedObjectCreated}
	pendingKey := generatedPendingObjectKey(objectKey)
	pendingOptions := minio.PutObjectOptions{ContentType: "application/octet-stream", DisableMultipart: true}
	pendingOptions.SetMatchETagExcept("*")
	if _, err := s.internal.PutObject(ctx, s.aiBucket, pendingKey, strings.NewReader(claim.Token), int64(len(claim.Token)), pendingOptions); err != nil {
		if isConditionalWriteConflict(err) {
			claim.State, claim.Token = ai.GeneratedObjectPending, ""
			return claim, nil
		}
		return ai.GeneratedObjectClaim{}, err
	}
	options := minio.PutObjectOptions{ContentType: mimeType, DisableMultipart: true}
	options.SetMatchETagExcept("*")
	_, err := s.internal.PutObject(ctx, s.aiBucket, objectKey, bytes.NewReader(value), int64(len(value)), options)
	if isConditionalWriteConflict(err) {
		if err := s.internal.RemoveObject(ctx, s.aiBucket, pendingKey, minio.RemoveObjectOptions{}); err != nil {
			return ai.GeneratedObjectClaim{}, err
		}
		claim.State, claim.Token = ai.GeneratedObjectCommitted, ""
		return claim, nil
	}
	if err != nil {
		_ = s.internal.RemoveObject(ctx, s.aiBucket, pendingKey, minio.RemoveObjectOptions{})
		return ai.GeneratedObjectClaim{}, err
	}
	return claim, nil
}

func (s *objectStore) CommitGenerated(ctx context.Context, claim ai.GeneratedObjectClaim) error {
	if claim.State != ai.GeneratedObjectCreated || !s.ownsGeneratedClaim(ctx, claim) {
		return fmt.Errorf("generated object claim is not owned")
	}
	return s.internal.RemoveObject(ctx, s.aiBucket, generatedPendingObjectKey(claim.ObjectKey), minio.RemoveObjectOptions{})
}

func (s *objectStore) CleanupGenerated(ctx context.Context, claim ai.GeneratedObjectClaim) error {
	if claim.State != ai.GeneratedObjectCreated || !s.ownsGeneratedClaim(ctx, claim) {
		return fmt.Errorf("generated object claim is not owned")
	}
	if err := s.internal.RemoveObject(ctx, s.aiBucket, claim.ObjectKey, minio.RemoveObjectOptions{}); err != nil {
		return err
	}
	return s.internal.RemoveObject(ctx, s.aiBucket, generatedPendingObjectKey(claim.ObjectKey), minio.RemoveObjectOptions{})
}

func (s *objectStore) DeleteGenerated(ctx context.Context, objectKey string) error {
	return s.internal.RemoveObject(ctx, s.aiBucket, objectKey, minio.RemoveObjectOptions{})
}

func (s *objectStore) GeneratedReadURL(ctx context.Context, objectKey string, expirySeconds int) (string, error) {
	if expirySeconds <= 0 || expirySeconds > 900 {
		return "", fmt.Errorf("generated read expiry is invalid")
	}
	if err := s.ensureGeneratedBucket(ctx); err != nil {
		return "", err
	}
	url, err := s.public.PresignedGetObject(ctx, s.aiBucket, objectKey, time.Duration(expirySeconds)*time.Second, nil)
	if err != nil {
		return "", err
	}
	return url.String(), nil
}

func (s *objectStore) ensureGeneratedBucket(ctx context.Context) error {
	exists, err := s.internal.BucketExists(ctx, s.aiBucket)
	if err != nil {
		return err
	}
	if !exists {
		if err := s.internal.MakeBucket(ctx, s.aiBucket, minio.MakeBucketOptions{Region: "us-east-1"}); err != nil {
			response := minio.ToErrorResponse(err)
			if response.Code != "BucketAlreadyOwnedByYou" && response.Code != "BucketAlreadyExists" && response.StatusCode != 409 {
				return err
			}
		}
	}
	return s.internal.SetBucketPolicy(ctx, s.aiBucket, generatedBucketPolicy)
}

func generatedPendingObjectKey(objectKey string) string {
	return objectKey + ".pending"
}

func (s *objectStore) ownsGeneratedClaim(ctx context.Context, claim ai.GeneratedObjectClaim) bool {
	object, err := s.internal.GetObject(ctx, s.aiBucket, generatedPendingObjectKey(claim.ObjectKey), minio.GetObjectOptions{})
	if err != nil {
		return false
	}
	defer object.Close()
	value, err := io.ReadAll(io.LimitReader(object, 128))
	return err == nil && string(value) == claim.Token
}

func isMissingObject(err error) bool {
	response := minio.ToErrorResponse(err)
	return response.Code == "NoSuchKey" || response.Code == "NoSuchObject" || response.StatusCode == 404
}

func isConditionalWriteConflict(err error) bool {
	if err == nil {
		return false
	}
	response := minio.ToErrorResponse(err)
	return response.Code == "PreconditionFailed" || response.StatusCode == 412
}
