package cmd

import (
	"context"
	"errors"
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

// getFactoryFromContext は内部的な共通取得ロジックです。
func getFactoryFromContext(ctx context.Context, key any, name string) (remoteio.IOFactory, error) {
	if val, ok := ctx.Value(key).(remoteio.IOFactory); ok && val != nil {
		return val, nil
	}
	return nil, fmt.Errorf("コンテキストに%sファクトリが見つかりません。", name)
}

func GetFactoryFromContext(ctx context.Context) (remoteio.IOFactory, error) {
	return getFactoryFromContext(ctx, gcsFactoryKey{}, "GCS")
}

func GetS3FactoryFromContext(ctx context.Context) (remoteio.IOFactory, error) {
	return getFactoryFromContext(ctx, s3FactoryKey{}, "S3")
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

// cleanupResources はリソースを解放します
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
				slog.Warn("クライアントのクローズに失敗しました", "client", t.name, "error", err)
			} else if verbose {
				slog.Info("クライアントをクローズしました。", "client", t.name)
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

	var gcsErr, s3Err error

	// 1. GCS Factory の初期化
	var gcsFactory remoteio.IOFactory
	gcsFactory, gcsErr = gcsfactory.New(initCtx)
	if gcsErr == nil {
		newCtx = context.WithValue(newCtx, gcsFactoryKey{}, gcsFactory)
		if verbose {
			slog.Info("GCS Factoryを初期化しました。")
		}
	}

	// 2. S3 Factory の初期化
	var s3Factory remoteio.IOFactory
	s3Factory, s3Err = s3factory.New(initCtx)
	if s3Err == nil {
		newCtx = context.WithValue(newCtx, s3FactoryKey{}, s3Factory)
		if verbose {
			slog.Info("S3 Factoryを初期化しました。")
		}
	}

	if gcsErr != nil && s3Err != nil {
		joinedErr := errors.Join(gcsErr, s3Err)
		return fmt.Errorf("GCS/S3 両方の初期化に失敗しました: %w", joinedErr)
	}

	cmd.SetContext(newCtx)
	return nil
}
