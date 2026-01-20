package cmd

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/shouni/go-remote-io/pkg/remoteio"
	"github.com/spf13/cobra"
)

// gcsCopyFlags は gcs-copy コマンド固有のフラグを保持します。
type gcsCopyFlags struct {
	OutputFilename string // -o, --output 出力ファイル名
}

var gcsFlags gcsCopyFlags // gcs-copy コマンド専用のフラグ変数

// gcsCopyCmd は 'gcs-copy' サブコマンドを定義します。
var gcsCopyCmd = &cobra.Command{
	Use:   "gcs-copy [source_path]",
	Short: "GCS/ローカルパス間で内容を読み込み、指定された GCS/ローカルパスへ転送します。",
	Long: `GCS URI (gs://) またはローカルファイルパスを扱います。
このコマンドは GCS専用ファクトリに依存し、S3 URIはサポートしません。`,
	Args: cobra.ExactArgs(1), // 1つのパス引数を必須とする
	RunE: runGCSCopy,
}

func init() {
	gcsCopyCmd.Flags().StringVarP(&gcsFlags.OutputFilename, "output", "o", "", "読み込んだ内容を書き出すファイル名（省略時は標準出力）")
}

// runGCSCopy は gcs-copy コマンドの実行ロジックです。
func runGCSCopy(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	inputPath := args[0] // 読み込むファイルパスまたはURI
	outputPath := gcsFlags.OutputFilename

	// 1. S3 URIチェック
	if remoteio.IsS3URI(inputPath) || (outputPath != "" && remoteio.IsS3URI(outputPath)) { // S3 URIはサポートしない
		return fmt.Errorf("このコマンドはS3 URI (s3://) をサポートしていません。s3-copy コマンドを使用してください。")
	}

	// 2. GCS専用Factoryの取得
	gcsFactory, err := GetFactoryFromContext(ctx)
	if err != nil {
		return err
	}

	// 3. InputReader の取得
	inputReader, err := gcsFactory.InputReader()
	if err != nil {
		return fmt.Errorf("InputReaderの作成に失敗しました: %w", err)
	}

	// 4. 読み込みストリームのオープン
	rc, err := inputReader.Open(ctx, inputPath)
	if err != nil {
		return fmt.Errorf("入力ストリームのオープンに失敗しました (%s): %w", inputPath, err)
	}
	defer rc.Close() // 読み込みストリームは必ずクローズする

	// 5. 出力先の決定とデータの転送
	if outputPath != "" {
		if remoteio.IsGCSURI(outputPath) {
			// GCS URIが指定された場合
			writer, err := gcsFactory.OutputWriter()
			if err != nil {
				return fmt.Errorf("OutputWriterの作成に失敗しました: %w", err)
			}

			if err := writer.Write(ctx, outputPath, rc, ""); err != nil {
				return fmt.Errorf("GCSへのコンテンツ書き込みに失敗しました: %w", err)
			}

			return nil

		} else {
			// ローカルファイルが指定された場合
			writer, err := gcsFactory.OutputWriter()
			if err != nil {
				return fmt.Errorf("OutputWriterの作成に失敗しました: %w", err)
			}

			// WriteToLocalにrcを渡して書き込みを実行
			if err := writer.WriteToLocal(ctx, outputPath, rc); err != nil {
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
