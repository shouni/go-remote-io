package cmd

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/shouni/clibase"
	"github.com/shouni/go-remote-io/pkg/remoteio"
	"github.com/spf13/cobra"
)

// gcsCopyFlags は gcs-copy コマンド固有のフラグを保持します。
type gcsCopyFlags struct {
	OutputFilename string // -o, --output 出力ファイル名
}

var gcsFlags gcsCopyFlags

// gcsCopyCmd は 'gcs-copy' サブコマンドを定義します。
var gcsCopyCmd = &cobra.Command{
	Use:   "gcs-copy [source_path]",
	Short: "GCS/ローカルパス間で内容を読み込み、指定された場所へ転送します。",
	Long: `GCS URI (gs://) またはローカルファイルパスを扱います。
このコマンドは GCS専用ファクトリに依存し、S3 URIはサポートしません。`,
	Args: cobra.ExactArgs(1),
	RunE: runGCSCopy,
}

func init() {
	gcsCopyCmd.Flags().StringVarP(&gcsFlags.OutputFilename, "output", "o", "", "読み込んだ内容を書き出すファイル名（省略時は標準出力）")
}

func runGCSCopy(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	inputPath := args[0]
	outputPath := gcsFlags.OutputFilename
	verbose := clibase.GetConfig().Verbose

	// 1. S3 URIチェック (早期リターン)
	if remoteio.IsS3URI(inputPath) || (outputPath != "" && remoteio.IsS3URI(outputPath)) {
		return fmt.Errorf("このコマンドはS3 URI (s3://) をサポートしていません。s3-copy コマンドを使用してください。")
	}

	// 2. GCS専用Factoryの取得 (修正済み: 名前変更に対応)
	gcsFactory, err := GetGCSFactoryFromContext(ctx)
	if err != nil {
		return err
	}

	// 3. 読み込みストリームのオープン
	inputReader, err := gcsFactory.InputReader()
	if err != nil {
		return fmt.Errorf("InputReaderの作成に失敗しました: %w", err)
	}

	rc, err := inputReader.Open(ctx, inputPath)
	if err != nil {
		return fmt.Errorf("入力ストリームのオープンに失敗しました (%s): %w", inputPath, err)
	}
	defer rc.Close()

	// 4. データ転送の実行
	if outputPath != "" {
		// 出力先が指定されている場合 (GCS or Local)
		writer, err := gcsFactory.OutputWriter()
		if err != nil {
			return fmt.Errorf("OutputWriterの作成に失敗しました: %w", err)
		}

		if verbose {
			slog.Info("データ転送開始",
				"input", inputPath,
				"output", outputPath,
				"mode", "file",
			)
		}

		// OutputWriter.Write は内部でローカル/GCSの判定を行うため、条件分岐を整理
		if err := writer.Write(ctx, outputPath, rc, ""); err != nil {
			return fmt.Errorf("出力先への書き込みに失敗しました (%s): %w", outputPath, err)
		}
	} else {
		// 標準出力に出力する場合
		if verbose {
			slog.Info("データ転送開始",
				"input", inputPath,
				"output", "stdout",
				"mode", "stream",
			)
		}

		if _, err := io.Copy(os.Stdout, rc); err != nil {
			return fmt.Errorf("標準出力への転送中にエラーが発生しました: %w", err)
		}
	}

	return nil
}
