package cmd

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/shouni/go-remote-io/pkg/remoteio"
	"github.com/spf13/cobra"
)

// RemoteReadFlags は remote-read コマンド固有のフラグを保持します。
type RemoteReadFlags struct {
	OutputFilename string // -o, --output 出力ファイル名
}

var remoteReadFlags RemoteReadFlags

// remoteReadCmd は 'remote-read' サブコマンドを定義します。
var remoteReadCmd = &cobra.Command{
	Use:   "remote-read [path]",
	Short: "指定されたパス（ローカルファイルまたは GCS URI）から内容を読み込み、標準出力またはファイルに出力します。",
	Long: `指定されたパスから io.ReadCloser を開きます。
パスが 'gs://' で始まっていれば GCS から、そうでなければローカルファイルとして読み込みます。
読み込みには ClientFactory から取得した InputReader を使用します。`,
	Args: cobra.ExactArgs(1), // 1つのパス引数を必須とする
	RunE: runRemoteRead,
}

func init() {
	// フラグの初期化
	remoteReadCmd.Flags().StringVarP(&remoteReadFlags.OutputFilename, "output", "o", "", "読み込んだ内容を書き出すファイル名（省略時は標準出力）")
}

// runRemoteRead は remote-read コマンドの実行ロジックです。
func runRemoteRead(cmd *cobra.Command, args []string) error {
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
	if remoteReadFlags.OutputFilename != "" {
		outputPath := remoteReadFlags.OutputFilename

		if remoteio.IsGCSURI(outputPath) {
			// GCS URIが指定された場合
			writer, err := clientFactory.NewOutputWriter()
			if err != nil {
				return fmt.Errorf("GCSOutputWriterの作成に失敗しました: %w", err)
			}

			// ★修正: writerがGCSOutputWriterインターフェースを満たすかチェック
			gcsWriter, ok := writer.(remoteio.GCSOutputWriter)
			if !ok {
				return fmt.Errorf("内部エラー: OutputWriterがGCSOutputWriterインターフェースを満たしていません")
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
				// ローカルファイルの書き込みに失敗した場合の汎用エラー
				return fmt.Errorf("LocalOutputWriterの作成に失敗しました: %w", err)
			}

			// ★修正: writerがLocalOutputWriterインターフェースを満たすかチェック
			localWriter, ok := writer.(remoteio.LocalOutputWriter)
			if !ok {
				// Factoryが返す具象型は、GCSまたはLocalのいずれか（または両方）のインターフェースを満たしている必要があります。
				return fmt.Errorf("内部エラー: OutputWriterがLocalOutputWriterインターフェースを満たしていません")
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
