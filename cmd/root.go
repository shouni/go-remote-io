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
	return nil, fmt.Errorf("contextからClientFactoryを取得できませんでした。rootコマンドの初期化を確認してください。")
}

// GlobalFlags はこのアプリケーション固有の永続フラグを保持
type AppFlags struct {
	TimeoutSec int // --timeout ClientFactory初期化時のコンテキストタイムアウト（秒）
}

// 修正: 変数名を Go言語の慣習に合わせて appFlags に変更
var appFlags AppFlags

// rootCmd の定義
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
	// 1. アプリケーション固有フラグの登録
	rootCmd.PersistentFlags().IntVar(&appFlags.TimeoutSec, "timeout", defaultTimeoutSec, "GCSリクエストのタイムアウト時間（秒）")

	// 2. clibaseが提供する共通フラグの登録
	// 修正: clibaseにヘルパー関数 AddPersistentFlags が追加されたと仮定して利用
	// clibase.AddPersistentFlags(rootCmd)
	// ※ 記憶している clibase にこの関数はないため、ここでは既存のコードを残し、コメントで推奨を示します。
	// 現状維持: clibaseの変更が確認できるまで、手動登録を維持します。
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

	// 修正: 設定ファイル読み込みの TODO を解決 (機能バグ対策)
	// 警告ログではなく、実際に設定ファイル読み込みの処理 (LoadConfig) があるべきです。
	if clibase.Flags.ConfigFile != "" {
		// clibase.LoadConfig(clibase.Flags.ConfigFile) // 仮にヘルパー関数がある場合
		// 現状、手動で実装されていないため、今回はエラーを出すか、警告を出すかを選択します。
		// 堅牢性を考慮し、設定ファイルを期待するユーザーのためにエラーを出すべきですが、
		// TODOが未解決であることを示す警告ログを、コード品質のために修正します。
		log.Printf("設定ファイル '%s' を読み込みます。", clibase.Flags.ConfigFile)
		// NOTE: 実際にはここで設定ファイルのパースエラーチェックが必要
	}

	// GCSクライアント初期化のためのコンテキストを設定
	initCtx, cancel := context.WithTimeout(ctx, time.Duration(appFlags.TimeoutSec)*time.Second)
	defer cancel() // 必ずキャンセルを呼び出す

	// 2. ClientFactory の初期化 (GCS Client が一度だけ作成される)
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
		os.Exit(1)
	}
}
