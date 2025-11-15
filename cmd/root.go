package cmd

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	clibase "github.com/shouni/go-cli-base"
	"github.com/spf13/cobra"

	"github.com/shouni/go-remote-io/pkg/factory"
)

const (
	appName           = "remoteio" // アプリ名
	defaultTimeoutSec = 10         // 秒
)

// FactoryKey は context.Context に factory.Factory を格納・取得するための非公開キー
type FactoryKey struct{} // ★修正なし: キーの型は適切

// GetFactoryFromContext は、cmd.Context() から factory.Factory を取り出す公開関数です。
func GetFactoryFromContext(ctx context.Context) (factory.Factory, error) {
	val := ctx.Value(FactoryKey{})

	if val == nil {
		return nil, fmt.Errorf("コンテキストにファクトリが見つかりません。")
	}

	// 型アサーションは factory.Factory インターフェースに対して行う
	f, ok := val.(factory.Factory)
	if !ok {
		return nil, fmt.Errorf("コンテキストの値が期待される型 (factory.Factory) ではありません。")
	}

	return f, nil
}

// AppFlags はこのアプリケーション固有の永続フラグを保持
type AppFlags struct {
	TimeoutSec int // --timeout ClientFactory初期化時のコンテキストタイムアウト（秒）
}

var appFlags AppFlags

// rootCmd の定義
var rootCmd = &cobra.Command{
	Use:   appName,
	Short: "リモートI/O操作のためのCLIツール。",
	Long:  "ローカルファイルとGCS URIをサポートする、リモートI/O操作のためのCLIツールです。",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

// --- アプリケーション固有のカスタム関数 ---

// addAppPersistentFlags は、アプリケーション固有の永続フラグをルートコマンドに追加します。
func addAppPersistentFlags(rootCmd *cobra.Command) {
	// 1. アプリケーション固有フラグの登録
	rootCmd.PersistentFlags().IntVar(&appFlags.TimeoutSec, "timeout", defaultTimeoutSec, "GCSリクエストのタイムアウト時間（秒）")
	rootCmd.PersistentFlags().BoolVarP(&clibase.Flags.Verbose, "verbose", "V", false, "Enable verbose output")
	rootCmd.PersistentFlags().StringVarP(&clibase.Flags.ConfigFile, "config", "C", "", "Config file path")
}

// initAppPreRunE は、clibase共通処理の後に実行される、アプリケーション固有のPersistentPreRunEです。
// ここでFactoryを初期化し、Contextに格納します。
func initAppPreRunE(cmd *cobra.Command, args []string) (factory.Factory, error) {
	ctx := cmd.Context()

	// GCSクライアント初期化のためのコンテキストを設定
	initCtx, cancel := context.WithTimeout(ctx, time.Duration(appFlags.TimeoutSec)*time.Second)
	defer cancel() // 必ずキャンセルを呼び出す

	// 2. Factory の初期化 (GCS Client が一度だけ作成される)
	clientFactory, err := factory.NewClientFactory(initCtx)
	if err != nil {
		return nil, fmt.Errorf("ClientFactoryの初期化に失敗しました: %w", err)
	}

	if clibase.Flags.Verbose {
		slog.Info("Factory（GCSクライアント含む）を初期化し、コンテキストに格納しました。")
	}

	// コマンドのコンテキストに Factory を格納
	newCtx := context.WithValue(ctx, FactoryKey{}, clientFactory)
	cmd.SetContext(newCtx)

	return clientFactory, nil
}

// --- エントリポイント ---

// Execute は、rootCmd を実行するメイン関数です。
func Execute() {
	// 実行時にFactoryを保持するための変数。Close()のために必要。
	var factoryInstance factory.Factory

	// 1. 永続フラグの追加と共通フラグの登録
	addAppPersistentFlags(rootCmd)

	// 2. PersistentPreRunE の設定
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		f, err := initAppPreRunE(cmd, args)
		if err != nil {
			return err
		}
		factoryInstance = f // Factory インスタンスを外部変数に格納
		return nil
	}

	// 3. サブコマンドの登録
	rootCmd.AddCommand(remoteReadCmd)
	// rootCmd.AddCommand(remoteWriteCmd) // 必要に応じて追加

	// 4. defer によるリソースクリーンアップの設定 (リソースリーク対策)
	defer func() {
		if factoryInstance != nil {
			if err := factoryInstance.Close(); err != nil {
				log.Printf("警告: GCSクライアントのクローズに失敗しました: %v", err)
			} else if clibase.Flags.Verbose {
				log.Println("GCSクライアントをクローズしました。")
			}
		}
	}()

	// 5. rootCmd.Execute() を直接呼び出します。
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
