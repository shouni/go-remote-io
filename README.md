# 📁 Go Remote IO

[![Language](https://img.shields.io/badge/Language-Go-blue)](https://golang.org/)
[![Go Version](https://img.shields.io/github/go-mod/go-version/shouni/go-remote-io)](https://golang.org/)
[![GitHub tag (latest by date)](https://img.shields.io/github/v/tag/shouni/go-remote-io)](https://github.com/shouni/go-remote-io/tags)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Go Remote IO は、**Google Cloud Storage (GCS) オブジェクト**と**ローカルファイルシステム**への I/O 操作を統一的に扱うための Go 言語製ライブラリです。

このライブラリは、アプリケーションの I/O 依存性を抽象化し、ビジネスロジックから GCS とローカルファイルの判別ロジックを分離します。

## ✨ 主要な機能と特徴

* **リソース管理とDI (`package factory` が担当)**: `factory.Factory` インターフェースを提供し、**`cloud.google.com/go/storage.Client`** の初期化、リソースライフサイクル管理（`Close()`）、およびI/Oコンポーネントの生成を統一的に行います。
* **統一された入力インターフェース**: `remoteio.InputReader` インターフェースを提供し、URI (例: `gs://bucket/object`) またはローカルファイルパスのどちらが渡されても、ファクトリを介して透過的に `io.ReadCloser` を開きます。
* **GCSストリーム書き込み (強化)**: `remoteio.GCSOutputWriter` は `io.Reader` を受け取り、コンテンツを直接 GCS バケットへ**ストリーミング書き込み**します。
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

### 3\. 利用方法（GCSOutputWriter の例）

`factory.NewOutputWriter()` を使用して `GCSOutputWriter` を取得します。

```go
package main

import (
    "bytes"
    "context"
    "log"
    
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
    
    // 2. GCSOutputWriter の実装を取得
    writer, err := clientFactory.NewOutputWriter()
    if err != nil {
        log.Fatalf("OutputWriter生成失敗: %v", err)
    }
    
    // 3. 書き込むデータとメタデータの準備
    content := "これはGCSにアップロードされるテストコンテンツです。"
    bucketName := "my-output-bucket"
    objectPath := "output/result.txt"
    contentType := "" // 空文字列を指定すると、"text/plain; charset=utf-8" が適用される
    
    reader := bytes.NewReader([]byte(content))
    
    // 4. GCSへの書き込み実行
    // writerは remoteio.GCSOutputWriter インターフェースを実装している
    log.Printf("GCSへ書き込み開始: gs://%s/%s", bucketName, objectPath)
    if err := writer.WriteToGCS(ctx, bucketName, objectPath, reader, contentType); err != nil {
        log.Fatalf("GCSへの書き込みに失敗しました: %v", err)
    }
    log.Println("GCSへの書き込みが完了しました。")
}
```

-----

## 📐 ライブラリ構成

CLIアプリケーションのエントリポイントを含む、再利用可能なパッケージ構成です。

```
go-remote-io/
├── go.mod
├── go.sum
├── README.md
├── pkg/
│   ├── remoteio/
│   │   ├── reader.go   # InputReader インターフェースと LocalGCSInputReader の実装
│   │   └── writer.go   # GCSOutputWriter インターフェースと GCSFileWriter の実装
│   └── factory/
│       └── factory.go   # Factory インターフェースと ClientFactory によるDIとリソース管理
└── cmd/ 
    └── root.go          # CLIアプリケーション (remoteio) のエントリポイント
```

### 外部依存パッケージ

本ライブラリは、以下の主要な外部パッケージに依存しています。

* **GCSコア依存**: `cloud.google.com/go/storage` (Google Cloud Storage へのアクセス)
* **CLI依存**: `github.com/spf13/cobra` および `github.com/shouni/go-cli-base` (`cmd/` パッケージで使用)

-----

### 📜 ライセンス (License)

このプロジェクトは [MIT License](https://opensource.org/licenses/MIT) の下で公開されています。
