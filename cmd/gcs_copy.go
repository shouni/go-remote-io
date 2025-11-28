package cmd

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/shouni/go-remote-io/pkg/remoteio"
	"github.com/spf13/cobra"
)

// rcopyFlags は rcopy コマンド固有のフラグを保持します。
type rcopyFlags struct {
	OutputFilename string // -o, --output 出力ファイル名
}

var flags rcopyFlags // フラグ変数の名前を 'flags' に変更

// rcopyCmd は 'rcopy' サブコマンドを定義します。
var rcopyCmd = &cobra.Command{
	Use:   "rcopy [source_path]", // コマンド名を rcopy に変更
	Short: "リモート/ローカルパス間で内容を読み込み、指定された出力先へ転送します。",
	Long: `指定されたパス (ローカルファイル、または GCS URI) から io.ReadCloser を開きます。
読み込んだ内容は、標準出力、ローカルファイル、または GCS URIで指定されたリモートパスへ転送されます。`,
	Args: cobra.ExactArgs(1), // 1つのパス引数を必須とする
	RunE: runRcopy,           // 実行関数名を runRcopy に変更
}

func init() {
	// フラグの初期化
	rcopyCmd.Flags().StringVarP(&flags.OutputFilename, "output", "o", "", "読み込んだ内容を書き出すファイル名（省略時は標準出力）")
}

// runRcopy は rcopy コマンドの実行ロジックです。
func runRcopy(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	inputPath := args[0] // 読み込むファイルパスまたはURI

	// 1. ClientFactory の取得 (DI)
	clientFactory, err := GetFactoryFromContext(ctx)
	if err != nil {
		return err
	}

	// 2. InputReader の取得 (入力依存性の注入)
	inputReader, err := clientFactory.NewInputReader()
	if err != nil {
		return fmt.Errorf("InputReaderの作成に失敗しました: %w", err)
	}

	// 3. 読み込みストリームのオープン
	rc, err := inputReader.Open(ctx, inputPath)
	if err != nil {
		return fmt.Errorf("入力ストリームのオープンに失敗しました (%s): %w", inputPath, err)
	}
	defer rc.Close() // 読み込みストリームは必ずクローズする

	// 4. 出力先の決定とデータの転送
	if flags.OutputFilename != "" {
		outputPath := flags.OutputFilename

		if remoteio.IsGCSURI(outputPath) {
			// GCS URIが指定された場合
			writer, err := clientFactory.NewOutputWriter()
			if err != nil {
				return fmt.Errorf("GCSOutputWriterの作成に失敗しました: %w", err)
			}

			// writerがGCSOutputWriterインターフェースを満たすかチェック
			gcsWriter, ok := writer.(remoteio.GCSOutputWriter)
			if !ok {
				return fmt.Errorf("FactoryがGCS出力用のWriterインターフェース(remoteio.GCSOutputWriter)を提供していません")
			}

			// URIをバケット名とオブジェクトパスにパース
			bucket, object, err := remoteio.ParseGCSURI(outputPath)
			if err != nil {
				return fmt.Errorf("GCS URIのパースに失敗しました: %w", err)
			}

			slog.Info("データ転送開始",
				slog.String("input", inputPath),
				slog.String("output", outputPath),
				slog.String("type", "GCS"),
			)

			if err := gcsWriter.WriteToGCS(ctx, bucket, object, rc, ""); err != nil {
				return fmt.Errorf("GCSへのコンテンツ書き込みに失敗しました: %w", err)
			}

			return nil

		} else {
			// ローカルファイルが指定された場合
			writer, err := clientFactory.NewOutputWriter()
			if err != nil {
				return fmt.Errorf("LocalOutputWriterの作成に失敗しました: %w", err)
			}

			// writerがLocalOutputWriterインターフェースを満たすかチェック
			localWriter, ok := writer.(remoteio.LocalOutputWriter)
			if !ok {
				return fmt.Errorf("Factoryがローカルファイル出力用のWriterインターフェース(remoteio.LocalOutputWriter)を提供していません")
			}

			slog.Info("データ転送開始",
				slog.String("input", inputPath),
				slog.String("output", outputPath),
				slog.String("type", "LocalFile"),
			)

			// WriteToLocalにrcを渡して書き込みを実行
			if err := localWriter.WriteToLocal(ctx, outputPath, rc); err != nil {
				return fmt.Errorf("ローカルファイルへの書き込みに失敗しました: %w", err)
			}

			return nil
		}
	} else {
		// 標準出力に出力する場合
		writer := os.Stdout

		slog.Info("データ転送開始",
			slog.String("input", inputPath),
			slog.String("output", "stdout"),
			slog.String("type", "Stdout"),
		)

		// 5. 読み込みと書き込みの実行 (標準出力の場合)
		if _, err := io.Copy(writer, rc); err != nil {
			return fmt.Errorf("データの転送中にエラーが発生しました: %w", err)
		}
		return nil
	}
}
