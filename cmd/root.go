package cmd

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/shouni/go-remote-io/pkg/remoteio"
	"github.com/spf13/cobra"
)

// gcsCopyCmd は 'gcs-copy' サブコマンドを定義します。
var gcsCopyCmd = &cobra.Command{
	Use:   "gcs-copy [source_path]",
	Short: "GCS/ローカルパス間で内容を読み込み、指定された GCS/ローカルパスへ転送します。",
	Long: `GCS URI (gs://) またはローカルファイルパスを扱います。
このコマンドは GCS専用ファクトリに依存し、S3 URIはサポートしません。`,
	Args: cobra.ExactArgs(1),
	RunE: runGCSCopy,
}

func init() {
	// フラグは共通のものを使用
	gcsCopyCmd.Flags().StringVarP(&flags.OutputFilename, "output", "o", "", "読み込んだ内容を書き出すファイル名（省略時は標準出力）")
}

// runGCSCopy は gcs-copy コマンドの実行ロジックです。
func runGCSCopy(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	inputPath := args[0]

	// 1. GCS専用 Factory の取得
	clientFactory, err := GetFactoryFromContext(ctx)
	if err != nil {
		return err
	}

	// 2. InputReader の取得
	inputReader, err := clientFactory.NewInputReader()
	if err != nil {
		return fmt.Errorf("InputReaderの作成に失敗しました: %w", err)
	}

	// 3. 読み込みストリームのオープン
	rc, err := inputReader.Open(ctx, inputPath)
	if err != nil {
		return fmt.Errorf("入力ストリームのオープンに失敗しました (%s): %w", inputPath, err)
	}
	defer rc.Close()

	// 4. 出力先の決定とデータの転送
	outputPath := flags.OutputFilename

	if outputPath == "" {
		// 標準出力に出力する場合
		slog.Info("データ転送開始", slog.String("input", inputPath), slog.String("output", "stdout"), slog.String("type", "Stdout"))
		if _, err := io.Copy(os.Stdout, rc); err != nil {
			return fmt.Errorf("データの転送中にエラーが発生しました: %w", err)
		}
		return nil
	}

	// S3 URIチェック (このコマンドはS3を扱わないためエラーにする)
	if remoteio.IsS3URI(inputPath) || remoteio.IsS3URI(outputPath) {
		return fmt.Errorf("このコマンドはS3 URI (s3://) をサポートしていません。s3-copy コマンドを使用してください。")
	}

	// --- GCS / ローカルファイルに出力する場合 ---
	writer, err := clientFactory.NewOutputWriter()
	if err != nil {
		return fmt.Errorf("OutputWriterの作成に失敗しました: %w", err)
	}

	if remoteio.IsGCSURI(outputPath) {
		// GCS URIが指定された場合
		gcsWriter, ok := writer.(remoteio.GCSOutputWriter)
		if !ok {
			return fmt.Errorf("FactoryがGCS出力用のWriterインターフェース(remoteio.GCSOutputWriter)を提供していません")
		}
		bucket, object, _ := remoteio.ParseGCSURI(outputPath)
		slog.Info("データ転送開始", slog.String("input", inputPath), slog.String("output", outputPath), slog.String("type", "GCS"))

		if err := gcsWriter.WriteToGCS(ctx, bucket, object, rc, ""); err != nil {
			return fmt.Errorf("GCSへのコンテンツ書き込みに失敗しました: %w", err)
		}

	} else {
		// ローカルファイルが指定された場合
		localWriter, ok := writer.(remoteio.LocalOutputWriter)
		if !ok {
			return fmt.Errorf("Factoryがローカルファイル出力用のWriterインターフェース(remoteio.LocalOutputWriter)を提供していません")
		}
		slog.Info("データ転送開始", slog.String("input", inputPath), slog.String("output", outputPath), slog.String("type", "LocalFile"))

		if err := localWriter.WriteToLocal(ctx, outputPath, rc); err != nil {
			return fmt.Errorf("ローカルファイルへの書き込みに失敗しました: %w", err)
		}
	}

	return nil
}
