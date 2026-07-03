package remoteio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func (r *UniversalInputReader) openS3(ctx context.Context, s3URI string) (io.ReadCloser, error) {
	if r.s3Client == nil {
		return nil, fmt.Errorf("S3クライアントが未初期化です (URI: %s)", s3URI)
	}
	bucketName, objectPath, err := ParseRemoteURI(s3URI)
	if err != nil {
		return nil, err
	}
	if objectPath == "" {
		return nil, fmt.Errorf("オブジェクト名が空です: %s", s3URI)
	}

	result, err := r.s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectPath),
	})
	if err != nil {
		if _, ok := errors.AsType[*types.NoSuchKey](err); ok {
			return nil, fmt.Errorf("S3オブジェクトが見つかりません (URI: %s): %w", s3URI, os.ErrNotExist)
		}
		return nil, fmt.Errorf("S3読み込み失敗 (URI: %s): %w", s3URI, err)
	}
	return result.Body, nil
}

func (r *UniversalInputReader) listS3(ctx context.Context, s3URI string, callback func(string) error) error {
	if r.s3Client == nil {
		return fmt.Errorf("S3クライアントが未初期化です (URI: %s)", s3URI)
	}
	bucketName, prefix, err := ParseRemoteURI(s3URI)
	if err != nil {
		return err
	}

	paginator := s3.NewListObjectsV2Paginator(r.s3Client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
		Prefix: aws.String(prefix),
	})

	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("S3リスト取得失敗 (ページネーション中, URI: %s): %w", s3URI, err)
		}
		for _, obj := range output.Contents {
			if *obj.Key == prefix {
				continue
			}
			fullPath := BuildS3URI(bucketName, *obj.Key)
			if err := callback(fullPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *UniversalInputReader) existsS3(ctx context.Context, s3URI string) (bool, error) {
	if r.s3Client == nil {
		return false, fmt.Errorf("S3クライアントが未初期化です: %s", s3URI)
	}
	bucketName, objectPath, err := ParseRemoteURI(s3URI)
	if err != nil {
		return false, err
	}

	_, err = r.s3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectPath),
	})
	if err == nil {
		return true, nil
	}

	if _, ok := errors.AsType[*types.NoSuchKey](err); ok {
		return false, nil
	}
	if _, ok := errors.AsType[*types.NotFound](err); ok {
		return false, nil
	}

	return false, fmt.Errorf("S3属性取得失敗: %w", err)
}
