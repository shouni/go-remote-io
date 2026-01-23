package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/shouni/clibase"
	"github.com/shouni/go-remote-io/pkg/gcsfactory"
	"github.com/shouni/go-remote-io/pkg/remoteio"
	"github.com/shouni/go-remote-io/pkg/s3factory"
	"github.com/spf13/cobra"
)

const (
	appName           = "remoteio"
	defaultTimeoutSec = 10
)

// =================================================================
// 1. グローバル変数とコンテキストキーの定義
// =================================================================

type gcsFactoryKey struct{}
type s3FactoryKey struct{}
type gcsFactoryCloserKey struct{}

type AppFlags struct {
	TimeoutSec int
}

var appFlags AppFlags

// =================================================================
// 2. Factoryの取得ヘルパー関数
// =================================================================

func GetFactoryFromContext(ctx context.Context) (remoteio.IOFactory, error) {
	if val, ok := ctx.Value(gcsFactoryKey{}).(remoteio.IOFactory); ok {
		return val, nil
	}
	return nil, fmt.Errorf("コンテキストにGCSファクトリが見つかりません。")
}

func GetS3FactoryFromContext(ctx context.Context) (remoteio.IOFactory, error) {
	if val, ok := ctx.Value(s3FactoryKey{}).(remoteio.IOFactory); ok {
		return val, nil
	}
	return nil, fmt.Errorf("コンテキストにS3ファクトリが見つかりません。")
}

// =================================================================
// 3. エントリポイント
// =================================================================

func Execute() {
	// クリーンアップ用の参照を保持するための変数を定義
	var cleanupCtx context.Context

	clibase.Execute(clibase.App{
		Name:     appName,
		AddFlags: addAppPersistentFlags,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			err := initPersistentPreRunE(cmd, args)
			cleanupCtx = cmd.Context() // クリーンアップ用に更新されたContextを保存
			return err
		},
		Commands: []*cobra.Command{
			gcsCopyCmd,
			s3CopyCmd,
		},
	})

	// リソースクリーンアップ (Executeが終了した後に実行)
	if cleanupCtx != nil {
		if closer, ok := cleanupCtx.Value(gcsFactoryCloserKey{}).(io.Closer); ok && closer != nil {
			if err := closer.Close(); err != nil {
				slog.Warn("GCSクライアントのクローズに失敗しました", slog.Any("error", err))
			} else if clibase.GetConfig().Verbose {
				slog.Info("GCSクライアントをクローズしました。")
			}
		}
	}
}

// =================================================================
// 4. ロジック実装
// =================================================================

func addAppPersistentFlags(rootCmd *cobra.Command) {
	rootCmd.PersistentFlags().IntVar(&appFlags.TimeoutSec, "timeout", defaultTimeoutSec, "リモートリクエストのタイムアウト時間（秒）")
}

func initPersistentPreRunE(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	initCtx, cancel := context.WithTimeout(ctx, time.Duration(appFlags.TimeoutSec)*time.Second)
	defer cancel()

	newCtx := ctx
	verbose := clibase.GetConfig().Verbose

	// 1. GCS Factory の初期化
	gcsFactory, gcsErr := gcsfactory.New(initCtx)
	if gcsErr == nil {
		newCtx = context.WithValue(newCtx, gcsFactoryKey{}, gcsFactory)
		newCtx = context.WithValue(newCtx, gcsFactoryCloserKey{}, io.Closer(gcsFactory))
		if verbose {
			slog.Info("GCS Factoryを初期化しました。", slog.String("client_type", "GCS"))
		}
	} else if verbose {
		slog.Warn("GCS Factoryの初期化をスキップしました。", slog.String("error", gcsErr.Error()))
	}

	// 2. S3 Factory の初期化
	s3Factory, s3Err := s3factory.New(initCtx)
	if s3Err == nil {
		newCtx = context.WithValue(newCtx, s3FactoryKey{}, s3Factory)
		if verbose {
			slog.Info("S3 Factoryを初期化しました。", slog.String("client_type", "S3"))
		}
	} else if verbose {
		slog.Warn("S3 Factoryの初期化をスキップしました。", slog.String("error", s3Err.Error()))
	}

	if gcsErr != nil && s3Err != nil {
		return fmt.Errorf("GCSファクトリとS3ファクトリの両方の初期化に失敗しました: GCS: %v, S3: %v", gcsErr, s3Err)
	}

	cmd.SetContext(newCtx)
	return nil
}
