package cmd

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/shouni/go-cli-base"
	"github.com/spf13/cobra"

	"github.com/shouni/go-remote-io/pkg/factory"
)

const (
	appName           = "remoteio" // アプリ名を remoteio に変更
	defaultTimeoutSec = 10         // 秒
)

// FactoryKey は context.Context に *factory.ClientFactory を格納・取得するための非公開キー
type FactoryKey struct{}

// GetClientFactory は、cmd.Context() から *factory.ClientFactory を取り出す公開関数です。
func GetClientFactory(ctx context.Context) (*factory.ClientFactory, error) {
	if f, ok := ctx.Value(FactoryKey{}).(*factory.ClientFactory); ok {
		return f, nil
	}
	// GCSクライアントは必須ではない場合もあるため、エラーメッセージを調整
	return nil, fmt.Errorf("contextからClientFactoryを取得できませんでした。rootコマンドの初期化を確認してください。")
}

// GlobalFlags はこのアプリケーション固有の永続フラグを保持
type AppFlags struct {
	TimeoutSec int // --timeout GCSクライアント初期化用 (使用しないが残す)
}

var Flags AppFlags // アプリケーション固有フラグにアクセスするためのグローバル変数

// --- アプリケーション固有のカスタム関数 ---

// addAppPersistentFlags は、アプリケーション固有の永続フラグをルートコマンドに追加します。
func addAppPersistentFlags(rootCmd *cobra.Command) {
	// フラグの追加ロジック...
	rootCmd.PersistentFlags().IntVar(&Flags.TimeoutSec, "timeout", defaultTimeoutSec, "GCSリクエストのタイムアウト時間（秒）")
}

// initAppPreRunE は、clibase共通処理の後に実行される、アプリケーション固有のPersistentPreRunEです。
// ここでClientFactoryを初期化し、Contextに格納します。
func initAppPreRunE(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// GCSクライアント初期化のためのコンテキストを設定
	initCtx, cancel := context.WithTimeout(ctx, time.Duration(Flags.TimeoutSec)*time.Second)
	defer cancel() // 必ずキャンセルを呼び出す

	// 1. ClientFactory の初期化 (ここで GCS Client が一度だけ作成される)
	clientFactory, err := factory.NewClientFactory(initCtx)
	if err != nil {
		return fmt.Errorf("ClientFactoryの初期化に失敗しました: %w", err)
	}

	if clibase.Flags.Verbose {
		log.Printf("ClientFactory（GCSクライアント含む）を初期化し、コンテキストに格納しました。")
	}

	// コマンドのコンテキストに ClientFactory を格納
	newCtx := context.WithValue(ctx, FactoryKey{}, clientFactory)
	cmd.SetContext(newCtx)

	return nil
}

// --- エントリポイント ---

// Execute は、rootCmd を実行するメイン関数です。
func Execute() {
	// 実行時にFactoryを保持するためのポインタ。Close()のために必要。
	var factoryInstance *factory.ClientFactory

	// clibase.Execute はエラーを返すのではなく、内部でos.Exit(1)するため、
	// 呼び出しを代入文にせず、エラーチェックも省略します。
	// エラー処理は clibase.Execute 内部に委譲されます。
	clibase.Execute( // 修正: 代入文を削除
		appName,
		addAppPersistentFlags,
		func(cmd *cobra.Command, args []string) error {
			// clibase共通のPersistentPreRunE処理を実行
			if err := initAppPreRunE(cmd, args); err != nil {
				return err
			}

			// ContextからFactoryを取得し、外部スコープの変数に格納（Execute後にCloseするため）
			f, err := GetClientFactory(cmd.Context())
			if err == nil {
				// 成功した場合のみファクトリをセット
				factoryInstance = f
			}
			return nil
		},
		// サブコマンドを登録
		remoteReadCmd,
		// remoteWriteCmd,
	)

	// GCSクライアントのリソースを解放
	if factoryInstance != nil {
		if err := factoryInstance.Close(); err != nil {
			log.Printf("警告: GCSクライアントのクローズに失敗しました: %v", err)
		} else if clibase.Flags.Verbose {
			log.Println("GCSクライアントをクローズしました。")
		}
	}
}
