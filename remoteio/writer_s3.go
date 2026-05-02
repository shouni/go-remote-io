package remoteio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func (w *UniversalIOWriter) writeS3(ctx context.Context, bucketName, objectPath string, contentReader io.Reader, contentType string) error {
	if bucketName == "" {
		return fmt.Errorf("S3への書き込みに失敗しました: バケット名が空です")
	}
	if objectPath == "" {
		return fmt.Errorf("S3への書き込みに失敗しました: オブジェクトパスが空です")
	}
	if w.s3Client == nil {
		return fmt.Errorf("S3への書き込みに失敗しました: S3クライアントが初期化されていません")
	}

	targetURI := BuildS3URI(bucketName, objectPath)
	slog.Info("S3書き込み処理開始", slog.String("uri", targetURI), slog.String("content_type", contentType))

	if contentType == "" {
		contentType = DefaultContentType
	}

	_, err := w.s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucketName),
		Key:         aws.String(objectPath),
		Body:        contentReader,
		ContentType: aws.String(contentType),
	})
	if err != nil {
		slog.Error("S3へのコンテンツ書き込み中にエラーが発生", slog.String("uri", targetURI), slog.String("error", err.Error()))
		return fmt.Errorf("S3へのコンテンツ書き込み中にエラーが発生しました: %w", err)
	}

	slog.Info("S3書き込み処理完了", slog.String("uri", targetURI))
	return nil
}

// WriteToS3 は S3 への直接書き込みを行います。
func (w *UniversalIOWriter) WriteToS3(ctx context.Context, bucketName, objectPath string, contentReader io.Reader, contentType string) error {
	return w.writeS3(ctx, bucketName, objectPath, contentReader, contentType)
}

func (w *UniversalIOWriter) deleteS3(ctx context.Context, bucketName, objectPath string) error {
	if w.s3Client == nil {
		return errors.New("S3クライアントが未初期化です")
	}
	_, err := w.s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectPath),
	})
	if err != nil {
		return fmt.Errorf("S3オブジェクトの削除に失敗しました: %w", err)
	}
	return nil
}
