package cmd

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/shouni/go-remote-io/pkg/remoteio"
	"github.com/spf13/cobra"
)

// s3CopyFlags は s3CopyCmd 固有のフラグを保持します。
type s3CopyFlags struct {
	OutputFilename string // -o, --output 出力ファイル名
	ContentType    string // -t, --content-type S3に書き込む際のMIMEタイプ
}

var s3Flags s3CopyFlags // s3CopyCmd 専用のフラグ変数

// =================================================================
// コマンド実装
// =================================================================

// s3CopyCmd は 's3-copy' サブコマンドを定義します。
var s3CopyCmd = &cobra.Command{
	Use:   "s3-copy [source_path]",
	Short: "S3/ローカルパス間で内容を読み込み、指定された S3/ローカルパスへ転送します。",
	Long: `S3 URI (s3://) またはローカルファイルパスを扱います。
このコマンドは S3専用ファクトリに依存し、GCS URIはサポートしません。`,
	Args: cobra.ExactArgs(1),
	RunE: runS3Copy,
}

func init() {
	// 固有のフラグ変数 s3Flags を使用するように修正
	s3CopyCmd.Flags().StringVarP(&s3Flags.OutputFilename, "output", "o", "", "読み込んだ内容を書き出すファイル名（省略時は標準出力）")
	s3CopyCmd.Flags().StringVarP(&s3Flags.ContentType, "content-type", "t", "", "S3に書き込む際のMIMEタイプ（例: text/plain; charset=utf-8）")
}

// runS3Copy は s3-copy コマンドの実行ロジックです。
func runS3Copy(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	inputPath := args[0]

	// 1. S3専用 Factory の取得 (GetS3FactoryFromContext は root.go に定義されている前提)
	s3Factory, err := GetS3FactoryFromContext(ctx)
	if err != nil {
		return err
	}

	// 2. InputReader の取得
	inputReader, err := s3Factory.NewInputReader()
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
	outputPath := s3Flags.OutputFilename

	if outputPath == "" {
		// 標準出力に出力する場合 (ロジック変更なし)
		slog.Info("データ転送開始", slog.String("input", inputPath), slog.String("output", "stdout"), slog.String("type", "Stdout"))
		if _, err := io.Copy(os.Stdout, rc); err != nil {
			return fmt.Errorf("データの転送中にエラーが発生しました: %w", err)
		}
		return nil
	}

	// GCS URIチェック (ロジック変更なし)
	if remoteio.IsGCSURI(inputPath) || remoteio.IsGCSURI(outputPath) {
		return fmt.Errorf("このコマンドはGCS URI (gs://) をサポートしていません。gcs-copy コマンドを使用してください。")
	}

	// --- S3 / ローカルファイルに出力する場合 ---
	writer, err := s3Factory.NewOutputWriter()
	if err != nil {
		return fmt.Errorf("OutputWriterの作成に失敗しました: %w", err)
	}

	// ContentTypeの決定
	contentType := s3Flags.ContentType
	if contentType == "" && remoteio.IsS3URI(outputPath) {
		// S3 URIへの書き込みでContent-Typeが指定されていない場合、デフォルトを適用
		// remoteio.DefaultContentType は remoteio/writer.go で定義されている
		contentType = remoteio.DefaultContentType
		slog.Debug("Content-Typeが未指定のため、デフォルト値を適用", slog.String("content_type", contentType))
	}

	if remoteio.IsS3URI(outputPath) {
		// S3 URIが指定された場合
		s3Writer, ok := writer.(remoteio.S3OutputWriter)
		if !ok {
			return fmt.Errorf("FactoryがS3出力用のWriterインターフェース(remoteio.S3OutputWriter)を提供していません")
		}
		bucket, object, _ := remoteio.ParseS3URI(outputPath)
		slog.Info("データ転送開始", slog.String("input", inputPath), slog.String("output", outputPath), slog.String("type", "S3"))

		// ContentTypeを渡す
		if err := s3Writer.WriteToS3(ctx, bucket, object, rc, contentType); err != nil {
			return fmt.Errorf("S3へのコンテンツ書き込みに失敗しました: %w", err)
		}

	} else {
		// ローカルファイルに出力する場合
		if err := writer.WriteToLocal(ctx, outputPath, rc); err != nil {
			return fmt.Errorf("ローカルファイルへの書き込みに失敗しました: %w", err)
		}
	}

	return nil
}
