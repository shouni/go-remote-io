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

// SignURL は S3 URI に対応する署名付き URL を生成します。
//
// remoteio.Signer の実装です。スキームは厳格で s3:// のみ、
// メソッドは GET / PUT のみをサポートします。
func (h *Handler) SignURL(ctx context.Context, uri, method string, expires time.Duration) (string, error) {
	bucket, key, err := h.parseObjectURI(uri)
	if err != nil {
		return "", fmt.Errorf("署名付きURLの生成に失敗しました: %w", err)
	}

	presign := awss3.NewPresignClient(h.client)
	switch strings.ToUpper(method) {
	case "GET":
		request, err := presign.PresignGetObject(ctx, &awss3.GetObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		}, awss3.WithPresignExpires(expires))
		if err != nil {
			return "", fmt.Errorf("S3 GET署名付きURLの生成に失敗しました (URI: %s): %w", uri, err)
		}
		return request.URL, nil
	case "PUT":
		request, err := presign.PresignPutObject(ctx, &awss3.PutObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		}, awss3.WithPresignExpires(expires))
		if err != nil {
			return "", fmt.Errorf("S3 PUT署名付きURLの生成に失敗しました (URI: %s): %w", uri, err)
		}
		return request.URL, nil
	default:
		return "", fmt.Errorf("サポートされていないHTTPメソッドです: %s (GETまたはPUTのみサポート): %w", method, remoteio.ErrNotSupported)
	}
}
