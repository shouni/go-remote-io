# 📁 Go Remote IO

[![Language](https://img.shields.io/badge/Language-Go-blue)](https://golang.org/)
[![Go Version](https://img.shields.io/github/go-mod/go-version/shouni/go-remote-io)](https://golang.org/)
[![GitHub tag (latest by date)](https://img.shields.io/github/v/tag/shouni/go-remote-io)](https://github.com/shouni/go-remote-io/tags)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Go Remote IO は、**Google Cloud Storage (GCS) オブジェクト**と**ローカルファイルシステム**への I/O 操作を統一的に扱うための Go 言語製ライブラリです。

このライブラリは、アプリケーションの I/O 依存性を抽象化し、ビジネスロジックから GCS とローカルファイルの判別ロジックを分離します。

**主要な機能と特徴 (`package remoteio`):**

* **統一された入力インターフェース**: `InputReader` インターフェースを提供し、URI (例: `gs://bucket/object`) またはローカルファイルパスのどちらが渡されても透過的に `io.ReadCloser` を開きます。
* **GCS書き込み**: `GCSOutputWriter` により、アプリケーションコンテンツを直接 GCS バケットへストリーム書き込みできます。
* **関心事の分離**: 外部サービスアクセス (`storage.Client`) の初期化は外部のファクトリに依存しますが、I/Oロジック自体は純粋にこのパッケージ内で完結します。

-----

## 🛠️ インストールと利用

### 1\. ライブラリのインストール

Goモジュールとして、以下のコマンドでプロジェクトに追加します。

```bash
go get github.com/shouni/go-remote-io
```

### 2\. 利用方法（InputReader の例）

`InputReader` を利用することで、パス文字列のプレフィックス判定（`gs://`）ロジックをアプリケーションから分離できます。

```go
package main

import (
    "context"
    "fmt"
    "io"
    "log"
    "os"
    
    "cloud.google.com/go/storage" // GCSクライアント
    "github.com/shouni/go-remote-io/remoteio" // I/Oロジック
)

func main() {
    ctx := context.Background()

    // 1. GCSクライアントの初期化（これは通常、ファクトリで行う）
    gcsClient, err := storage.NewClient(ctx)
    if err != nil {
        log.Fatalf("GCSクライアント初期化失敗: %v", err)
    }
    defer gcsClient.Close()
    
    // 2. remoteio.InputReader の実装を取得
    reader := remoteio.NewLocalGCSInputReader(gcsClient)
    
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

-----

## 📐 ライブラリ構成

CLIアプリケーションではなく、再利用可能なパッケージとして機能が特化しています。

```
go-remote-io/
├── go.mod
├── go.sum
├── README.md
├── remoteio/
│   ├── reader.go   # InputReader インターフェースと LocalGCSInputReader の実装
│   └── writer.go   # GCSOutputWriter インターフェースと GCSFileWriter の実装
└── cmd/ (オプション: テスト/デモ用 CLI)
    └── root.go
```

### 外部依存パッケージ

本ライブラリは、以下の主要な外部パッケージに依存しています。

* **`cloud.google.com/go/storage`**: Google Cloud Storage へのアクセスを処理します。

-----

### 📜 ライセンス (License)

このプロジェクトは [MIT License](https://opensource.org/licenses/MIT) の下で公開されています。
