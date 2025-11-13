package cmd

import (
	"context"
	"fmt"
	"log"

	"cloud.google.com/go/storage"
	"github.com/shouni/go-cli-base"
	"github.com/spf13/cobra"
)

const (
	appName           = "remoteio" // アプリ名を remoteio に変更
	defaultTimeoutSec = 10         // 秒
)

// gcsClientKey は context.Context に *storage.Client を格納・取得するための非公開キー
type gcsClientKey struct{}

// GetGCSClient は、cmd.Context() から *storage.Client を取り出す公開関数です。
func GetGCSClient(ctx context.Context) (*storage.Client, error) {
	if client, ok := ctx.Value(gcsClientKey{}).(*storage.Client); ok {
		return client, nil
	}
	// GCSクライアントは必須ではない場合もあるため、エラーメッセージを調整
	return nil, fmt.Errorf("contextからGCSクライアントを取得できませんでした。rootコマンドの初期化を確認してください。")
}

// GlobalFlags はこのアプリケーション固有の永続フラグを保持
type AppFlags struct {
	TimeoutSec int // --timeout GCSクライアント初期化用
}

var Flags AppFlags // アプリケーション固有フラグにアクセスするためのグローバル変数

// --- アプリケーション固有のカスタム関数 ---

// addAppPersistentFlags は、アプリケーション固有の永続フラグをルートコマンドに追加します。
func addAppPersistentFlags(rootCmd *cobra.Command) {
	// GCSクライアントの初期化に特化したフラグのみを残す
	rootCmd.PersistentFlags().IntVar(&Flags.TimeoutSec, "timeout", defaultTimeoutSec, "GCSリクエストのタイムアウト時間（秒）")
	// Note: Title, Message, HTTP Clientの初期化は不要なため削除
}

// initAppPreRunE は、clibase共通処理の後に実行される、アプリケーション固有のPersistentPreRunEです。
func initAppPreRunE(cmd *cobra.Command, args []string) error {
	// GCSクライアントの初期化
	ctx := cmd.Context()

	// GCSクライアントの初期化（タイムアウトはコンテキストに影響）
	// Note: GCSクライアントの初期化自体にタイムアウトは直接適用されないが、
	// NewClientが内部で使用する認証情報の取得等でコンテキストが使われるため、ここではコンテキストを更新しない。
	// GCSの操作は、各コマンドのRunEでGetGCSClient経由で行う。

	// GCSクライアントを初期化し、コンテキストに格納
	gcsClient, err := storage.NewClient(ctx) // GCSクライアントを初期化
	if err != nil {
		return fmt.Errorf("GCSクライアントの初期化に失敗しました: %w", err)
	}

	if clibase.Flags.Verbose {
		log.Printf("GCSクライアントを初期化しました。")
	}

	// コマンドのコンテキストに GCS Client を格納
	newCtx := context.WithValue(ctx, gcsClientKey{}, gcsClient)
	cmd.SetContext(newCtx)

	return nil
}

// --- エントリポイント ---

// Execute は、rootCmd を実行するメイン関数です。
func Execute() {
	// ここでは、サブコマンドとして GCS I/Oのテスト用コマンドを登録する
	clibase.Execute(
		appName,
		addAppPersistentFlags,
		initAppPreRunE,
		// 例として、リモートリードコマンドを登録 (まだ作成していません)
		// remoteReadCmd,
		// remoteWriteCmd,
	)
}
