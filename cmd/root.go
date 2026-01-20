package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	clibase "github.com/shouni/go-cli-base"
	"github.com/shouni/go-remote-io/pkg/remoteio"
	"github.com/spf13/cobra"

	"github.com/shouni/go-remote-io/pkg/gcsfactory"
	"github.com/shouni/go-remote-io/pkg/s3factory"
)

const (
	appName           = "remoteio" // アプリ名
	defaultTimeoutSec = 10         // 秒
)

// =================================================================
// 1. グローバル変数とコンテキストキーの定義
// =================================================================

// gcsFactoryKey は context.Context に gcsfactory.Factory (GCS専用) を格納・取得するための非公開キー
type gcsFactoryKey struct{}

// s3FactoryKey は context.Context に s3factory.Factory (S3専用) を格納・取得するための非公開キー
type s3FactoryKey struct{}

// AppFlags はこのアプリケーション固有の永続フラグを保持
type AppFlags struct {
	TimeoutSec int // --timeout ClientFactory初期化時のコンテキストタイムアウト（秒）
}

var appFlags AppFlags

// gcsFactoryCloserKey は GCS Factoryのクローズ処理のために io.Closer を保持するキー
type gcsFactoryCloserKey struct{}

// =================================================================
// 2. Factoryの取得ヘルパー関数
// =================================================================

// GetFactoryFromContext は、cmd.Context() から gcsfactory.Factory (GCS専用) を取り出します。
func GetFactoryFromContext(ctx context.Context) (remoteio.IOFactory, error) {
	val := ctx.Value(gcsFactoryKey{})
	if val == nil {
		return nil, fmt.Errorf("コンテキストにGCSファクトリが見つかりません。")
	}
	f, ok := val.(remoteio.IOFactory)
	if !ok {
		return nil, fmt.Errorf("コンテキストの値が期待される型 (factory.Factory) ではありません。")
	}
	return f, nil
}

// GetS3FactoryFromContext は、cmd.Context() から s3factory.Factory (S3専用) を取り出します。
func GetS3FactoryFromContext(ctx context.Context) (remoteio.IOFactory, error) {
	val := ctx.Value(s3FactoryKey{})
	if val == nil {
		return nil, fmt.Errorf("コンテキストにS3ファクトリが見つかりません。")
	}
	f, ok := val.(remoteio.IOFactory)
	if !ok {
		return nil, fmt.Errorf("コンテキストの値が期待される型 (s3factory.Factory) ではありません。")
	}
	return f, nil
}

// =================================================================
// 3. ルートコマンドの定義
// =================================================================

// rootCmd の定義
var rootCmd = &cobra.Command{
	Use:   appName,
	Short: "リモートI/O操作のためのCLIツール。",
	Long:  "ローカルファイルとGCS/S3 URIをサポートする、リモートI/O操作のためのCLIツールです。",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

// addAppPersistentFlags は、アプリケーション固有の永続フラグをルートコマンドに追加します。
func addAppPersistentFlags(rootCmd *cobra.Command) {
	rootCmd.PersistentFlags().IntVar(&appFlags.TimeoutSec, "timeout", defaultTimeoutSec, "リモートリクエストのタイムアウト時間（秒）")
}

// initPersistentPreRunE は、GCSファクトリとS3ファクトリの両方を初期化し、Contextに格納します。
func initPersistentPreRunE(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	initCtx, cancel := context.WithTimeout(ctx, time.Duration(appFlags.TimeoutSec)*time.Second)
	defer cancel()

	newCtx := ctx

	// 1. GCS Factory の初期化 (GCS Client)
	gcsFactory, gcsErr := gcsfactory.New(initCtx)
	if gcsErr == nil {
		// コンテキストに GCS Factory を格納
		newCtx = context.WithValue(newCtx, gcsFactoryKey{}, gcsFactory)
		// Close() のために GCS Factory を格納 (io.Closerを実装しているため)
		newCtx = context.WithValue(newCtx, gcsFactoryCloserKey{}, io.Closer(gcsFactory))

		if clibase.Flags.Verbose {
			slog.Info("GCS Factoryを初期化しました。", slog.String("client_type", "GCS"))
		}
	} else if clibase.Flags.Verbose {
		slog.Warn("GCS Factoryの初期化をスキップしました。", slog.String("error", gcsErr.Error()))
	}

	// 2. S3 Factory の初期化 (S3 Client)
	s3Factory, s3Err := s3factory.New(initCtx)
	if s3Err == nil {
		// コンテキストに S3 Factory を格納
		newCtx = context.WithValue(newCtx, s3FactoryKey{}, s3Factory)
		if clibase.Flags.Verbose {
			slog.Info("S3 Factoryを初期化しました。", slog.String("client_type", "S3"))
		}
	} else if clibase.Flags.Verbose {
		slog.Warn("S3 Factoryの初期化をスキップしました。", slog.String("error", s3Err.Error()))
	}

	// どちらのファクトリも初期化に失敗した場合はエラーを返す
	if gcsErr != nil && s3Err != nil {
		return fmt.Errorf("GCSファクトリとS3ファクトリの両方の初期化に失敗しました: GCS: %v, S3: %v", gcsErr, s3Err)
	}

	// 3. Context の更新とクリーンアップ用のインスタンス保持
	cmd.SetContext(newCtx)

	return nil
}

// =================================================================
// 4. エントリポイント
// =================================================================

// Execute は、rootCmd を実行するメイン関数です。
func Execute() {
	// 1. 永続フラグの追加
	addAppPersistentFlags(rootCmd)

	// 2. PersistentPreRunE の設定 (両方のファクトリをコンテキストに注入)
	rootCmd.PersistentPreRunE = initPersistentPreRunE

	// 3. サブコマンドの登録
	rootCmd.AddCommand(gcsCopyCmd)
	rootCmd.AddCommand(s3CopyCmd)

	// 4. defer によるリソースクリーンアップの設定 (リソースリーク対策)
	defer func() {
		if closer, ok := rootCmd.Context().Value(gcsFactoryCloserKey{}).(io.Closer); ok && closer != nil {
			if err := closer.Close(); err != nil {
				slog.Warn("GCSクライアントのクローズに失敗しました", slog.Any("error", err))
			} else if clibase.Flags.Verbose {
				slog.Info("GCSクライアントをクローズしました。")
			}
		}
	}()

	// 5. rootCmd.Execute() を直接呼び出します。
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
