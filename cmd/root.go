package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	clibase "github.com/shouni/go-cli-base"
	"github.com/spf13/cobra"

	// go.modが参照する正しいパス
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
	// 修正: コメントを実態に合わせて修正
	TimeoutSec int // --timeout ClientFactory初期化時のコンテキストタイムアウト（秒）
}

// 修正: 変数名を Go言語の慣習に合わせて appFlags に変更
var appFlags AppFlags // アプリケーション固有フラグにアクセスするためのグローバル変数

// rootCmd の定義 (Execute() 内で初期化されるため、ここでは最低限の定義)
var rootCmd = &cobra.Command{
	Use:   appName,
	Short: "A CLI tool for remote I/O operations.",
	Long:  "The CLI tool for remote I/O operations, supporting local files and GCS URIs.",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

// --- アプリケーション固有のカスタム関数 ---

// addAppPersistentFlags は、アプリケーション固有の永続フラグをルートコマンドに追加します。
func addAppPersistentFlags(rootCmd *cobra.Command) {
	// アプリケーション固有フラグの登録
	// 修正: 参照箇所を appFlags.TimeoutSec に変更
	rootCmd.PersistentFlags().IntVar(&appFlags.TimeoutSec, "timeout", defaultTimeoutSec, "GCSリクエストのタイムアウト時間（秒）")

	// clibaseが提供する共通フラグをここで手動で追加します。
	// 修正: 保守性のためのコメントを追加
	// Note: clibaseライブラリに AddPersistentFlags のようなヘルパー関数があれば、それを利用することを強く推奨します。
	rootCmd.PersistentFlags().BoolVarP(&clibase.Flags.Verbose, "verbose", "V", false, "Enable verbose output")
	rootCmd.PersistentFlags().StringVarP(&clibase.Flags.ConfigFile, "config", "C", "", "Config file path")
}

// initAppPreRunE は、clibase共通処理の後に実行される、アプリケーション固有のPersistentPreRunEです。
// ここでClientFactoryを初期化し、Contextに格納します。
func initAppPreRunE(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// 1. clibase 共通の PersistentPreRun 処理 (手動で実行)
	if clibase.Flags.Verbose {
		log.Printf("Verboseモードが有効です。")
	}

	// 修正: 設定ファイル読み込みロジックの TODO を追加 (機能不全対策)
	// TODO: clibase.Flags.ConfigFile が指定されている場合、設定ファイルを読み込むロジックを実装する
	if clibase.Flags.ConfigFile != "" {
		// ここで設定ファイルを読み込む処理を実行すべき。clibaseにヘルパー関数がない場合は手動で実装が必要。
		log.Printf("設定ファイル '%s' の読み込みをスキップしました。", clibase.Flags.ConfigFile)
	}

	// GCSクライアント初期化のためのコンテキストを設定
	// 修正: 参照箇所を appFlags.TimeoutSec に変更
	initCtx, cancel := context.WithTimeout(ctx, time.Duration(appFlags.TimeoutSec)*time.Second)
	defer cancel() // 必ずキャンセルを呼び出す

	// 2. ClientFactory の初期化 (ここで GCS Client が一度だけ作成される)
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
// defer を確実に実行するため、clibase.Execute の使用を中止します。
func Execute() {
	// 実行時にFactoryを保持するためのポインタ。Close()のために必要。
	var factoryInstance *factory.ClientFactory

	// 1. 永続フラグの追加と共通フラグの登録
	addAppPersistentFlags(rootCmd)

	// 2. PersistentPreRunE の設定 (clibase.Executeが担っていた役割をここで実装)
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		// initAppPreRunE の実行と factoryInstance への格納を担う
		if err := initAppPreRunE(cmd, args); err != nil {
			return err
		}

		// ContextからFactoryを取得し、外部スコープの変数に格納（Execute後にCloseするため）
		f, err := GetClientFactory(cmd.Context())
		if err == nil {
			factoryInstance = f
		}
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
		// cobra.Command.Execute() はエラーを返すため、ここで適切に処理し os.Exit(1)
		os.Exit(1)
	}
}
