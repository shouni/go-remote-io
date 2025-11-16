# 📁 Go Remote IO

[![Language](https://img.shields.io/badge/Language-Go-blue)](https://golang.org/)
[![Go Version](https://img.shields.io/github/go-mod/go-version/shouni/go-remote-io)](https://golang.org/)
[![GitHub tag (latest by date)](https://img.shields.io/github/v/tag/shouni/go-remote-io)](https://github.com/shouni/go-remote-io/tags)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Go Remote IO は、**Google Cloud Storage (GCS) オブジェクト**と**ローカルファイルシステム**への I/O 操作を統一的に扱うための Go 言語製ライブラリです。

このライブラリは、アプリケーションの I/O 依存性を抽象化し、ビジネスロジックから GCS とローカルファイルの判別ロジックを分離します。

-----

## ✨ 主要な機能と特徴

* **リソース管理とDI (`package factory` が担当)**: `factory.Factory` インターフェースを提供し、**`cloud.google.com/go/storage.Client`** の初期化、リソースライフサイクル管理（`Close()`）、およびI/Oコンポーネントの生成を統一的に行います。
* **統一された入力インターフェース**: `remoteio.InputReader` インターフェースを提供し、URI (例: `gs://bucket/object`) またはローカルファイルパスのどちらが渡されても、ファクトリを介して透過的に `io.ReadCloser` を開きます。
* **統一された出力インターフェース (更新)**: **`remoteio.OutputWriter`** インターフェースを提供します。これは、**`GCSOutputWriter`** と **`LocalOutputWriter`** の両方を満たす汎用的な契約であり、アプリケーションのビジネスロジックは出力先の種類（GCSかローカルか）を知る必要がありません。
* **GCSストリーム書き込み**: `remoteio.GCSOutputWriter` は `io.Reader` を受け取り、コンテンツを直接 GCS バケットへ**ストリーミング書き込み**します。これにより、大規模なデータ処理時のメモリ効率が向上します。また、**MIMEタイプを動的に指定**可能です（未指定の場合は `text/plain; charset=utf-8` がデフォルトで適用されます）。
* **関心事の分離**: 外部サービスアクセス (`storage.Client`) の初期化は外部のファクトリに依存し、I/Oロジック自体は純粋に `remoteio` パッケージ内で完結します。

-----

## 🛠️ インストールと利用

### 1\. ライブラリのインストール

Goモジュールとして、以下のコマンドでプロジェクトに追加します。

```bash
go get github.com/shouni/go-remote-io
```

### 2\. 利用方法（InputReader の例）

`factory.Factory` を初期化し、そこから **`NewInputReader()`** メソッドを使ってコンポーネントを取得します。

```go
package main

import (
    "context"
    "fmt"
    "io"
    "log"

    "github.com/shouni/go-remote-io/pkg/factory" 
)

func main() {
    ctx := context.Background()

    // 1. Factoryの初期化
    // GCSクライアントの初期化と管理をFactoryに委譲
    clientFactory, err := factory.NewClientFactory(ctx)
    if err != nil {
        log.Fatalf("Factory初期化失敗: %v", err)
    }
    // ★重要: FactoryのClose()をdeferで呼び出し、リソースを解放する
    defer func() {
        if closeErr := clientFactory.Close(); closeErr != nil {
            log.Printf("警告: Factoryのクローズに失敗しました: %v", closeErr)
        }
    }()
    
    // 2. InputReader の実装を取得
    reader, err := clientFactory.NewInputReader()
    if err != nil {
        log.Fatalf("InputReader生成失敗: %v", err)
    }
    
    // 3. ローカルファイル、または GCS URI のどちらでも利用可能
    paths := []string{"./local_file.txt", "gs://my-bucket/remote_data.csv"}

    for _, path := range paths {
        rc, err := reader.Open(ctx, path)
        if err != nil {
            log.Printf("読み込み失敗 (%s): %v", path, err)
            continue
        }
        defer rc.Close()
        
        content, _ := io.ReadAll(rc)
        fmt.Printf("--- 読み込み元: %s ---\n%s\n", path, string(content))
    }
}
```

### 3\. 利用方法（OutputWriter の例）

`factory.NewOutputWriter()` を使用して **`remoteio.OutputWriter`** を取得します。CLIなどの上位レイヤーでは、出力先URIに応じて適切なインターフェースにキャストして利用します。

```go
package main

import (
    "bytes"
    "context"
    "log"
    "strings"
    
    "github.com/shouni/go-remote-io/pkg/factory"
    "github.com/shouni/go-remote-io/pkg/remoteio" 
)

func main() {
    ctx := context.Background()

    // 1. Factoryの初期化とクローズ
    clientFactory, err := factory.NewClientFactory(ctx)
    if err != nil {
        log.Fatalf("Factory初期化失敗: %v", err)
    }
    defer func() {
        if closeErr := clientFactory.Close(); closeErr != nil {
            log.Printf("警告: Factoryのクローズに失敗しました: %v", closeErr)
        }
    }()
    
    // 2. OutputWriter の実装を取得（汎用インターフェース）
    outputURI := "gs://my-output-bucket/output/result.txt"
    
    rawWriter, err := clientFactory.NewOutputWriter()
    if err != nil {
        log.Fatalf("OutputWriter生成失敗: %v", err)
    }
    
    // 3. URIを判別し、適切なインターフェースにキャストして書き込み
    content := "これはテストコンテンツです。"
    reader := bytes.NewReader([]byte(content))

    if strings.HasPrefix(outputURI, "gs://") {
        // GCSの場合: GCSOutputWriterにキャスト
        writer, ok := rawWriter.(remoteio.GCSOutputWriter)
        if !ok {
            log.Fatalf("Factoryが GCSOutputWriter インターフェースを提供していません。")
        }
        
        // GCS URIをパースするロジック（実際には remoteio.ParseGCSURI を利用）
        bucketName := "my-output-bucket"
        objectPath := "output/result.txt"
        
        log.Printf("GCSへ書き込み開始: %s", outputURI)
        if err := writer.WriteToGCS(ctx, bucketName, objectPath, reader, "text/plain"); err != nil {
            log.Fatalf("GCSへの書き込みに失敗しました: %v", err)
        }
        log.Println("GCSへの書き込みが完了しました。")
        
    } else {
        // ローカルの場合: LocalOutputWriterにキャスト
        writer, ok := rawWriter.(remoteio.LocalOutputWriter)
        if !ok {
            log.Fatalf("Factoryが LocalOutputWriter インターフェースを提供していません。")
        }
        log.Printf("ローカルへ書き込み開始: %s", outputURI)
        if err := writer.WriteToLocal(ctx, outputURI, reader); err != nil {
             log.Fatalf("ローカルへの書き込みに失敗しました: %v", err)
        }
        log.Println("ローカルへの書き込みが完了しました。")
    }
}
```

-----

## 💻 CLI実行方法とデータ転送の例

**`rcopy`** サブコマンドは、入力元と出力先がローカルファイル、または GCS URI のいずれであっても、透過的なデータ転送を可能にします。

### 1\. 標準出力への転送 (GCS → Stdout)

入力元のデータをそのまま標準出力に出力します。

```bash
# コマンド例: GCSのファイルを標準出力に出力
$ go run ./ rcopy gs://input-bucket/data.txt
```

### 2\. ローカルファイルへの転送 (Local → Local)

ローカルファイルを読み込み、別のローカルファイルに書き出します。

```bash
# コマンド例: ローカルファイルをローカルファイルに転送
$ go run ./ rcopy ./local/data.csv -o ./output/result.csv
```

### 3\. GCSオブジェクトへの転送 (Local → GCS)

ローカルファイルを読み込み、GCSバケットへストリーミングで書き出します。

```bash
# コマンド例: ローカルファイルをGCSに転送
$ go run ./ rcopy ./local/report.json -o gs://dest-bucket/archive/report.json
```

### 4\. GCSからGCSへの転送 (GCS → GCS)

GCSオブジェクトから読み込み、別のGCSオブジェクトへ直接内容をストリーミング転送します。これは、**サーバーサイドでのパイプライン処理**に非常に有用です。

```bash
# コマンド例: GCSオブジェクト間での転送
$ go run ./ rcopy gs://source-bucket/file.dat -o gs://dest-bucket/archive/file.dat

# 実行ログの例
2025/11/16 03:39:25 INFO データ転送開始 input=gs://source-bucket/file.dat output=gs://dest-bucket/archive/file.dat type=GCS
```

-----

## 📐 ライブラリ構成

CLIアプリケーションのエントリポイントを含む、再利用可能なパッケージ構成です。

```
go-remote-io/
├── pkg/
│   ├── remoteio/
│   │   ├── reader.go   # InputReader インターフェースと LocalGCSInputReader の実装
│   │   ├── writer.go   # OutputWriter (GCS/Local) インターフェースと具象実装
│   │   └── uri.go      # GCS URI判定・パースユーティリティ (IsGCSURI, ParseGCSURI)
│   └── factory/
│       └── factory.go   # Factory インターフェースと ClientFactory によるDIとリソース管理
└── cmd/ 
    └── rcopy.go         # CLIアプリケーション (rcopy) のエントリポイント
    └── root.go          # CLIアプリケーションのルートコマンド定義
```

### 外部依存パッケージ

本ライブラリは、以下の主要な外部パッケージに依存しています。

* **GCSコア依存**: `cloud.google.com/go/storage` (Google Cloud Storage へのアクセス)
* **CLI依存**: `github.com/spf13/cobra` および `github.com/shouni/go-cli-base` (`cmd/` パッケージで使用)

-----

### 📜 ライセンス (License)

このプロジェクトは [MIT License](https://opensource.org/licenses/MIT) の下で公開されています。
