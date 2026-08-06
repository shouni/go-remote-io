package s3

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/shouni/go-remote-io/remoteio"
)

// urlSigner は S3 の署名付き URL を生成する remoteio.URLSigner です。
type urlSigner struct {
	client *awss3.PresignClient
}

var _ remoteio.URLSigner = (*urlSigner)(nil)

// NewURLSigner は S3 用の署名付き URL 生成器を初期化します。
func NewURLSigner(client *awss3.Client) remoteio.URLSigner {
	// AWS SDK v2 の NewPresignClient は nil を渡すとパニックするためガード
	var presignClient *awss3.PresignClient
	if client != nil {
		presignClient = awss3.NewPresignClient(client)
	}
	return &urlSigner{client: presignClient}
}

// GenerateSignedURL は S3 URI に対応する署名付きURLを生成します。
// スキームは厳格で s3:// のみ、メソッドは GET / PUT のみをサポートします。
func (s *urlSigner) GenerateSignedURL(ctx context.Context, path string, method string, expires time.Duration) (string, error) {
	if s.client == nil {
		return "", fmt.Errorf("S3クライアントが初期化されていないため、署名付きURLを生成できません")
	}

	if !remoteio.IsS3URI(path) {
		return "", fmt.Errorf("署名付きURLはS3 URI (s3://...) のみサポートされます: %s", path)
	}

	bucketName, objectPath, err := remoteio.ParseRemoteURI(path)
	if err != nil {
		return "", fmt.Errorf("S3 URIの解析に失敗しました: %w", err)
	}

	switch strings.ToUpper(method) {
	case "GET":
		request, err := s.client.PresignGetObject(ctx, &awss3.GetObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(objectPath),
		}, awss3.WithPresignExpires(expires))
		if err != nil {
			return "", fmt.Errorf("S3 GET署名付きURLの生成に失敗しました: %w", err)
		}
		return request.URL, nil
	case "PUT":
		request, err := s.client.PresignPutObject(ctx, &awss3.PutObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(objectPath),
		}, awss3.WithPresignExpires(expires))
		if err != nil {
			return "", fmt.Errorf("S3 PUT署名付きURLの生成に失敗しました: %w", err)
		}
		return request.URL, nil
	default:
		return "", fmt.Errorf("サポートされていないHTTPメソッドです: %s (GETまたはPUTのみサポート)", method)
	}
}
