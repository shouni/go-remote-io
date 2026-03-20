package remoteio

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// =================================================================
// GCS URLSigner の実装
// =================================================================

type gcsURLSigner struct {
	client *storage.Client
}

func NewGCSURLSigner(client *storage.Client) URLSigner {
	return &gcsURLSigner{client: client}
}

func (s *gcsURLSigner) GenerateSignedURL(ctx context.Context, uri string, method string, expires time.Duration) (string, error) {
	// クライアントの初期化チェック
	if s.client == nil {
		return "", fmt.Errorf("GCSクライアントが初期化されていないため、署名付きURLを生成できません")
	}

	if !IsGCSURI(uri) {
		return "", fmt.Errorf("署名付きURLはGCS URI (gs://...) のみサポートされます: %s", uri)
	}

	bucketName, objectPath, err := ParseGCSURI(uri)
	if err != nil {
		return "", fmt.Errorf("GCS URIの解析に失敗: %w", err)
	}

	opts := &storage.SignedURLOptions{
		Scheme:  storage.SigningSchemeV4,
		Method:  method,
		Expires: time.Now().Add(expires),
	}

	return s.client.Bucket(bucketName).SignedURL(objectPath, opts)
}

// =================================================================
// S3 URLSigner の実装
// =================================================================

type s3URLSigner struct {
	client *s3.PresignClient
}

func NewS3URLSigner(s3Client *s3.Client) URLSigner {
	// AWS SDK v2 の NewPresignClient は nil を渡すとパニックするためガード
	var presignClient *s3.PresignClient
	if s3Client != nil {
		presignClient = s3.NewPresignClient(s3Client)
	}
	return &s3URLSigner{
		client: presignClient,
	}
}

func (s *s3URLSigner) GenerateSignedURL(ctx context.Context, uri string, method string, expires time.Duration) (string, error) {
	// クライアントの初期化チェック
	if s.client == nil {
		return "", fmt.Errorf("S3クライアントが初期化されていないため、署名付きURLを生成できません")
	}

	if !IsS3URI(uri) {
		return "", fmt.Errorf("署名付きURLはS3 URI (s3://...) のみサポートされます: %s", uri)
	}

	bucketName, objectPath, err := ParseS3URI(uri)
	if err != nil {
		return "", fmt.Errorf("S3 URIの解析に失敗しました: %w", err)
	}

	switch strings.ToUpper(method) {
	case "GET":
		request, err := s.client.PresignGetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(objectPath),
		}, s3.WithPresignExpires(expires))
		if err != nil {
			return "", fmt.Errorf("S3 GET署名付きURLの生成に失敗しました: %w", err)
		}
		return request.URL, nil
	case "PUT":
		request, err := s.client.PresignPutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(objectPath),
		}, s3.WithPresignExpires(expires))
		if err != nil {
			return "", fmt.Errorf("S3 PUT署名付きURLの生成に失敗しました: %w", err)
		}
		return request.URL, nil
	default:
		return "", fmt.Errorf("サポートされていないHTTPメソッドです: %s (GETまたはPUTのみサポート)", method)
	}
}
