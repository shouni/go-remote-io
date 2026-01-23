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
// 1. コンテキストキーの定義
// =================================================================

type gcsFactoryKey struct{}
type s3FactoryKey struct{}

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
	clibase.Execute(clibase.App{
		Name:     appName,
		AddFlags: addAppPersistentFlags,
		PreRunE:  initPersistentPreRunE,
		PostRun: func(cmd *cobra.Command, args []string) {
			cleanupResources(cmd.Context())
		},

		Commands: []*cobra.Command{
			gcsCopyCmd,
			s3CopyCmd,
		},
	})
}

// cleanupResources はコンテキスト内の Factory が io.Closer を実装していればクローズします
func cleanupResources(ctx context.Context) {
	verbose := clibase.GetConfig().Verbose
	targets := []struct {
		key  any
		name string
	}{
		{gcsFactoryKey{}, "GCS"},
		{s3FactoryKey{}, "S3"},
	}

	for _, t := range targets {
		if closer, ok := ctx.Value(t.key).(io.Closer); ok && closer != nil {
			if err := closer.Close(); err != nil {
				slog.Warn(fmt.Sprintf("%sクライアントのクローズに失敗しました", t.name), slog.Any("error", err))
			} else if verbose {
				slog.Info(fmt.Sprintf("%sクライアントをクローズしました。", t.name))
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
		if verbose {
			slog.Info("GCS Factoryを初期化しました。")
		}
	} else if verbose {
		slog.Warn("GCS Factoryの初期化をスキップしました。", slog.Any("error", gcsErr))
	}

	// 2. S3 Factory の初期化
	s3Factory, s3Err := s3factory.New(initCtx)
	if s3Err == nil {
		newCtx = context.WithValue(newCtx, s3FactoryKey{}, s3Factory)
		if verbose {
			slog.Info("S3 Factoryを初期化しました。")
		}
	} else if verbose {
		slog.Warn("S3 Factoryの初期化をスキップしました。", slog.Any("error", s3Err))
	}

	// 両方失敗した場合のみエラー
	if gcsErr != nil && s3Err != nil {
		return fmt.Errorf("GCS/S3 両方の初期化に失敗しました: GCS: %v, S3: %v", gcsErr, s3Err)
	}

	cmd.SetContext(newCtx)
	return nil
}
