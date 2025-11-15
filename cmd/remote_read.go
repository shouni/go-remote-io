package cmd

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings" // GCS URIの判定に使用

	"github.com/spf13/cobra"
	// 依存パッケージのインポート (ClientFactory, remoteio.GCSOutputWriter, remoteio.InputReaderなど)
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
	var outputTarget string

	if remoteReadFlags.OutputFilename != "" {
		outputPath := remoteReadFlags.OutputFilename

		if strings.HasPrefix(outputPath, "gs://") {
			// GCS URIが指定された場合: GCSOutputWriterを使用し、io.CopyをWriteToGCS内で実行させる

			outputWriter, err := clientFactory.NewOutputWriter()
			if err != nil {
				return fmt.Errorf("GCSOutputWriterの作成に失敗しました: %w", err)
			}

			// URIをバケット名とオブジェクトパスにパース
			bucket, object, err := outputWriter.ParseGCSURI(outputPath)
			if err != nil {
				return fmt.Errorf("GCS URIのパースに失敗しました: %w", err)
			}

			outputTarget = outputPath
			slog.Info("読み込み元: %s -> 出力先(GCS): %s", inputPath, outputTarget)

			// ★修正: WriteToGCS に読み込みストリーム (rc) を渡して書き込みを実行させる
			// Content-Type はここでは空文字列を指定し、Writer側でデフォルト値が適用されるようにする
			if err := outputWriter.WriteToGCS(ctx, bucket, object, rc, ""); err != nil {
				return fmt.Errorf("GCSへのコンテンツ書き込みに失敗しました: %w", err)
			}

			// GCSへの書き込みが完了したため、ここで処理を終了する
			return nil

		} else {
			// ローカルファイルが指定された場合: os.Createを使用する
			file, err := os.Create(outputPath)
			if err != nil {
				return fmt.Errorf("出力ファイルの作成に失敗しました: %w", err)
			}
			defer file.Close()

			writer := file // ローカル書き込み用ライターを定義
			outputTarget = outputPath
			slog.Info("読み込み元: %s -> 出力先(ローカル): %s", inputPath, outputTarget)

			// 5. 読み込みと書き込みの実行 (ローカルファイルの場合)
			if _, err := io.Copy(writer, rc); err != nil {
				return fmt.Errorf("データの転送中にエラーが発生しました: %w", err)
			}
			return nil
		}
	} else {
		// 標準出力に出力する場合
		writer := os.Stdout // 標準出力ライターを定義
		outputTarget = "標準出力 (stdout)"
		slog.Info("読み込み元: %s -> 出力先: %s", inputPath, outputTarget)

		// 5. 読み込みと書き込みの実行 (標準出力の場合)
		if _, err := io.Copy(writer, rc); err != nil {
			return fmt.Errorf("データの転送中にエラーが発生しました: %w", err)
		}
		return nil
	}
}
