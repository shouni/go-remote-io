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

// s3CopyFlags は s3CopyCmd 固有のフラグを保持します。
type s3CopyFlags struct {
	OutputFilename string // -o, --output 出力ファイル名
	ContentType    string // -t, --content-type S3に書き込む際のMIMEタイプ
}

var s3Flags s3CopyFlags

// s3CopyCmd は 's3-copy' サブコマンドを定義します。
var s3CopyCmd = &cobra.Command{
	Use:   "s3-copy [source_path]",
	Short: "S3/ローカルパス間で内容を読み込み、指定された場所へ転送します。",
	Long: `S3 URI (s3://) またはローカルファイルパスを扱います。
このコマンドは S3専用ファクトリに依存し、GCS URIはサポートしません。`,
	Args: cobra.ExactArgs(1),
	RunE: runS3Copy,
}

func init() {
	s3CopyCmd.Flags().StringVarP(&s3Flags.OutputFilename, "output", "o", "", "読み込んだ内容を書き出すファイル名（省略時は標準出力）")
	s3CopyCmd.Flags().StringVarP(&s3Flags.ContentType, "content-type", "t", "", "S3に書き込む際のMIMEタイプ（例: text/plain; charset=utf-8）")
}

func runS3Copy(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	inputPath := args[0]
	outputPath := s3Flags.OutputFilename
	verbose := clibase.GetConfig().Verbose

	// 1. GCS URIチェック
	if remoteio.IsGCSURI(inputPath) || (outputPath != "" && remoteio.IsGCSURI(outputPath)) {
		return fmt.Errorf("このコマンドはGCS URI (gs://) をサポートしていません。gcs-copy コマンドを使用してください。")
	}

	// 2. S3専用 Factory の取得
	s3Factory, err := GetS3FactoryFromContext(ctx)
	if err != nil {
		return err
	}

	// 3. 読み込みストリームのオープン
	inputReader, err := s3Factory.InputReader()
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
		writer, err := s3Factory.OutputWriter()
		if err != nil {
			return fmt.Errorf("OutputWriterの作成に失敗しました: %w", err)
		}

		contentType := s3Flags.ContentType
		if contentType == "" && remoteio.IsS3URI(outputPath) {
			contentType = remoteio.DefaultContentType
			if verbose {
				slog.Debug("Content-Typeが未指定のため、デフォルト値を適用", "content_type", contentType)
			}
		}

		if verbose {
			slog.Info("データ転送開始",
				"input", inputPath,
				"output", outputPath,
				"mode", "file",
			)
		}

		if err := writer.Write(ctx, outputPath, rc, contentType); err != nil {
			targetType := "ローカルファイル"
			if remoteio.IsS3URI(outputPath) {
				targetType = "S3"
			}
			return fmt.Errorf("%sへの書き込みに失敗しました (%s): %w", targetType, outputPath, err)
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
