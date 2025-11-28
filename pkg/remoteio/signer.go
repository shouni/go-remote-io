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
// 1. インターフェース定義
// =================================================================

// URLSigner は、リモートストレージの署名付きURLを生成する機能を提供します。
type URLSigner interface {
	// GenerateSignedURL は、指定されたURIの署名付きURLを生成します。
	// ローカルパスなど、サポートされないURIの場合はエラーを返します。
	GenerateSignedURL(ctx context.Context, uri string, method string, expires time.Duration) (string, error)
}

// =================================================================
// 2. GCS URLSigner の実装
// =================================================================

// gcsURLSigner は GCS 用の URLSigner 実装です。
type gcsURLSigner struct {
	client *storage.Client
}

// NewGCSURLSigner は gcsURLSigner を初期化します。
func NewGCSURLSigner(client *storage.Client) URLSigner {
	return &gcsURLSigner{client: client}
}

// GenerateSignedURL は、GCS URIの署名付きURLを生成します。
func (s *gcsURLSigner) GenerateSignedURL(ctx context.Context, uri string, method string, expires time.Duration) (string, error) {
	// GCS URIでなければエラー
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
	// storage.Client を利用して署名
	return s.client.Bucket(bucketName).SignedURL(objectPath, opts)
}

// =================================================================
// 3. S3 URLSigner の実装
// =================================================================

// s3URLSigner は S3 用の URLSigner 実装です。
type s3URLSigner struct {
	client *s3.PresignClient
}

// NewS3URLSigner は s3URLSigner を初期化します。
// S3クライアントからプリサインクライアントを作成して注入します。
func NewS3URLSigner(s3Client *s3.Client) URLSigner {
	return &s3URLSigner{
		client: s3.NewPresignClient(s3Client),
	}
}

// GenerateSignedURL は、S3 URIの署名付きURLを生成します。
func (s *s3URLSigner) GenerateSignedURL(ctx context.Context, uri string, method string, expires time.Duration) (string, error) {
	// S3 URIでなければエラー (util.go の IsS3URI を使用)
	if !IsS3URI(uri) {
		return "", fmt.Errorf("署名付きURLはS3 URI (s3://...) のみサポートされます: %s", uri)
	}

	// util.go の ParseS3URI を使用
	bucketName, objectPath, err := ParseS3URI(uri)
	if err != nil {
		return "", fmt.Errorf("S3 URIの解析に失敗しました: %w", err)
	}

	// S3では GET と PUT のプリサインメソッドを分ける必要がある
	switch strings.ToUpper(method) {
	case "GET":
		// GetObject用の署名付きURLを生成
		request, err := s.client.PresignGetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(objectPath),
		}, s3.WithPresignExpires(expires))
		if err != nil {
			return "", fmt.Errorf("S3 GET署名付きURLの生成に失敗しました: %w", err)
		}
		return request.URL, nil
	case "PUT":
		// PutObject用の署名付きURLを生成 (アップロード用)
		request, err := s.client.PresignPutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(objectPath),
			// ContentTypeを固定する必要がある場合はここに追加可能
		}, s3.WithPresignExpires(expires))
		if err != nil {
			return "", fmt.Errorf("S3 PUT署名付きURLの生成に失敗しました: %w", err)
		}
		return request.URL, nil
	default:
		return "", fmt.Errorf("サポートされていないHTTPメソッドです: %s (GETまたはPUTのみサポート)", method)
	}
}
