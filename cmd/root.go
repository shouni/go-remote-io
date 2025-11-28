package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	clibase "github.com/shouni/go-cli-base"
	"github.com/spf13/cobra"

	"github.com/shouni/go-remote-io/pkg/factory"
	"github.com/shouni/go-remote-io/pkg/s3factory"
)

const (
	appName           = "remoteio" // アプリ名
	defaultTimeoutSec = 10         // 秒
)

// =================================================================
// 1. グローバル変数とコンテキストキーの定義
// =================================================================

// FactoryKey は context.Context に factory.Factory (GCS専用) を格納・取得するための非公開キー
type FactoryKey struct{}

// S3FactoryKey は context.Context に s3factory.Factory (S3専用) を格納・取得するための非公開キー
type S3FactoryKey struct{}

// AppFlags はこのアプリケーション固有の永続フラグを保持
type AppFlags struct {
	TimeoutSec int // --timeout ClientFactory初期化時のコンテキストタイムアウト（秒）
}

var appFlags AppFlags

// factoryInstanceKey は GCS Factoryを Close() するために保持するキー
type factoryInstanceKey struct{}

// =================================================================
// 2. Factoryの取得ヘルパー関数
// =================================================================

// GetFactoryFromContext は、cmd.Context() から factory.Factory (GCS専用) を取り出します。
func GetFactoryFromContext(ctx context.Context) (factory.Factory, error) {
	val := ctx.Value(FactoryKey{})
	if val == nil {
		return nil, fmt.Errorf("コンテキストにGCSファクトリが見つかりません。")
	}
	f, ok := val.(factory.Factory)
	if !ok {
		return nil, fmt.Errorf("コンテキストの値が期待される型 (factory.Factory) ではありません。")
	}
	return f, nil
}

// GetS3FactoryFromContext は、cmd.Context() から s3factory.Factory (S3専用) を取り出します。
func GetS3FactoryFromContext(ctx context.Context) (s3factory.Factory, error) {
	val := ctx.Value(S3FactoryKey{})
	if val == nil {
		return nil, fmt.Errorf("コンテキストにS3ファクトリが見つかりません。")
	}
	f, ok := val.(s3factory.Factory)
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
	var gcsFactory factory.Factory
	var s3Factory s3factory.Factory

	// 1. GCS Factory の初期化 (GCS Client)
	gcsFactory, gcsErr := factory.NewGCSClientFactory(initCtx)
	if gcsErr == nil {
		// コンテキストに GCS Factory を格納
		newCtx = context.WithValue(newCtx, FactoryKey{}, gcsFactory)
		// Close() のために GCS Factory を格納 (io.Closerを実装しているため)
		newCtx = context.WithValue(newCtx, factoryInstanceKey{}, io.Closer(gcsFactory))

		if clibase.Flags.Verbose {
			slog.Info("GCS Factoryを初期化しました。", slog.String("client_type", "GCS"))
		}
	} else if clibase.Flags.Verbose {
		slog.Warn("GCS Factoryの初期化をスキップしました。", slog.String("error", gcsErr.Error()))
	}

	// 2. S3 Factory の初期化 (S3 Client)
	s3Factory, s3Err := s3factory.NewS3ClientFactory(initCtx)
	if s3Err == nil {
		// コンテキストに S3 Factory を格納
		newCtx = context.WithValue(newCtx, S3FactoryKey{}, s3Factory)
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
		// GCS Factoryのクローズ処理 (S3のクローズ処理は削除済み)
		if closer, ok := rootCmd.Context().Value(factoryInstanceKey{}).(io.Closer); ok && closer != nil {
			if err := closer.Close(); err != nil {
				slog.Info("警告: GCSクライアントのクローズに失敗しました: %v", err)
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
