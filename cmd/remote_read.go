package cmd

import (
	"fmt"
	"io"
	"os"

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

	// 1. ClientFactory の取得
	clientFactory, err := GetFactoryFromContext(ctx)
	if err != nil {
		return err
	}

	// 2. InputReader の取得 (依存性の注入)
	inputReader, err := clientFactory.NewInputReader()
	if err != nil {
		return fmt.Errorf("InputReaderの作成に失敗しました: %w", err)
	}

	// 3. 読み込みストリームのオープン
	// InputReaderはパスを判断し、ローカルまたはGCSから読み込みストリームを開く
	rc, err := inputReader.Open(ctx, inputPath)
	if err != nil {
		return fmt.Errorf("入力ストリームのオープンに失敗しました (%s): %w", inputPath, err)
	}
	defer rc.Close() // 読み込みストリームは必ずクローズする

	// 4. 出力先の決定
	var writer io.Writer
	var outputTarget string

	if remoteReadFlags.OutputFilename != "" {
		// ファイルに出力する場合
		file, err := os.Create(remoteReadFlags.OutputFilename)
		if err != nil {
			return fmt.Errorf("出力ファイルの作成に失敗しました: %w", err)
		}
		defer file.Close()

		writer = file
		outputTarget = remoteReadFlags.OutputFilename
	} else {
		// 標準出力に出力する場合
		writer = os.Stdout
		outputTarget = "標準出力 (stdout)"
	}
	fmt.Fprintf(os.Stderr, "読み込み元: %s -> 出力先: %s\n", inputPath, outputTarget)

	// 5. 読み込みと書き込みの実行
	// io.Copy を使用して効率的にストリームを転送
	if _, err := io.Copy(writer, rc); err != nil {
		return fmt.Errorf("データの転送中にエラーが発生しました: %w", err)
	}

	return nil
}
