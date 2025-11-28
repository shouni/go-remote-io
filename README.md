# 📁 Go Remote IO

[![Language](https://img.shields.io/badge/Language-Go-blue)](https://golang.org/)
[![Go Version](https://img.shields.io/github/go-mod/go-version/shouni/go-remote-io)](https://golang.org/)
[![GitHub tag (latest by date)](https://img.shields.io/github/v/tag/shouni/go-remote-io)](https://github.com/shouni/go-remote-io/tags)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Go Remote IO は、**Google Cloud Storage (GCS)**、**Amazon S3**、および**ローカルファイルシステム**への I/O 操作を統一的に扱うための Go 言語製ライブラリです。

このライブラリは、アプリケーションの I/O 依存性を抽象化し、ビジネスロジックからクラウドサービスやローカルファイルの判別ロジックを分離します。

-----

## ✨ 主要な機能と特徴

* **ユニバーサル I/O**: URIのプレフィックスに応じて、**GCS (`gs://`)**、**Amazon S3 (`s3://`)**、または**ローカルファイルパス**へのI/O処理を**透過的に**切り替えます。
* **リソース管理とDI (`package factory` / `package s3factory` が担当)**:
    * **`factory`パッケージ**: **GCSクライアント**のみを初期化・管理し、GCP環境に特化します。
    * **`s3factory`パッケージ (新規)**: **S3クライアント**のみを初期化・管理し、AWS環境に特化することで、**認証情報の依存関係を完全に分離**します。
* **統一された入力インターフェース**: `remoteio.InputReader` インターフェースを提供し、URI (例: `gs://`, `s3://`) またはローカルファイルパスのどちらが渡されても、ファクトリを介して透過的に `io.ReadCloser` を開きます。
* **統一された出力インターフェース**: `remoteio.OutputWriter` インターフェースを提供します。このインターフェースは**汎用的な `Write(ctx, uri, reader, contentType)` メソッド**を核とし、GCS/S3/ローカルへの書き込みを**透過的に**処理します。
* **期限付きURLの生成**: `remoteio.URLSigner` インターフェースを提供します。GCS および S3 URIに対して**期限付きの署名付きURL (Signed URL)** を生成できます。
* **関心事の分離**: 外部サービスアクセス (`storage.Client`, `s3.Client`) の初期化は外部のファクトリに依存し、I/Oロジック自体は純粋に `remoteio` パッケージ内で完結します。

-----

## 🛠️ インストールと利用

### 1\. ライブラリのインストール

Goモジュールとして、以下のコマンドでプロジェクトに追加します。

```bash
go get github.com/shouni/go-remote-io
```

### 2\. 利用方法（InputReader の例）

使用するクラウド環境に合わせて、適切なファクトリを初期化します。

#### A. GCS環境での利用 (`factory`パッケージを使用)

```go
package main

import (
    "context"
    "fmt"
    "io"
    "log"

    "https://github.com/shouni/go-remote-io/pkg/factory" 
)

func main() {
    ctx := context.Background()

    // 1. GCS専用Factoryの初期化とクローズ
    clientFactory, err := factory.NewClientFactory(ctx)
    if err != nil {
        log.Fatalf("Factory初期化失敗: %v", err)
    }
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
    
    // 3. ローカルファイル、または GCS URI で利用可能
    paths := []string{"./local_file.txt", "gs://my-bucket/remote_data.csv"}

    for _, path := range paths {
        rc, err := reader.Open(ctx, path)
        if err != nil {
            log.Printf("読み込み失敗 (%s): %v", path, err)
            continue
        }
        
        content, _ := io.ReadAll(rc)
        fmt.Printf("--- 読み込み元: %s ---\n%s\n", path, string(content))
        rc.Close()
    }
}
```

#### B. S3環境での利用 (`s3factory`パッケージを使用)

S3専用のファクトリを使用することで、GCP関連の認証情報やSDKへの依存性を排除できます。

```go
package main

import (
    "context"
    "log"
    "strings"
    "https://github.com/shouni/go-remote-io/pkg/s3factory"
)

func main() {
    ctx := context.Background()
    
    // 1. S3専用Factoryの初期化 (AWS Configのみをロード)
    s3Factory, err := s3factory.NewS3ClientFactory(ctx)
    if err != nil {
        log.Fatalf("S3 Factory初期化失敗: %v", err)
    }
    
    // 2. OutputWriter を取得
    writer, _ := s3Factory.NewOutputWriter()
    
    // 3. S3とローカルファイルに書き込む
    s3URI := "s3://my-aws-bucket/output/result.txt"
    content := "S3専用環境からの書き込みです"
    
    if err := writer.Write(ctx, s3URI, strings.NewReader(content), "text/plain"); err != nil {
        log.Fatalf("S3への書き込みに失敗しました: %v", err)
    }
    log.Printf("✅ S3への書き込みが完了しました: %s", s3URI)
}
```

### 3\. 利用方法（URLSigner の例: 期限付きURLの生成）

GCSとS3のSignerは、それぞれのファクトリから取得します。

```go
package main

import (
    "context"
    "log"
    "time"
    "https://github.com/shouni/go-remote-io/pkg/factory"
    "https://github.com/shouni/go-remote-io/pkg/s3factory"
)

func main() {
    ctx := context.Background()
    
    // GCS Signerの取得
    gcsFactory, _ := factory.NewClientFactory(ctx)
    gcsSigner, _ := gcsFactory.NewGCSURLSigner()
    
    // S3 Signerの取得
    s3Factory, _ := s3factory.NewS3ClientFactory(ctx)
    s3Signer, _ := s3Factory.NewS3URLSigner()
    
    expires := 15 * time.Minute
    
    // GCS 署名付きURLを生成
    gcsSignedURL, _ := gcsSigner.GenerateSignedURL(ctx, "gs://my-bucket/report.pdf", "GET", expires)
    log.Printf("✅ GCS Signed URL: %s", gcsSignedURL)
    
    // S3 署名付きURLを生成
    s3SignedURL, _ := s3Signer.GenerateSignedURL(ctx, "s3://my-bucket/data.csv", "PUT", expires)
    log.Printf("✅ S3 Signed URL: %s", s3SignedURL)
}
```

-----

## 💻 CLI実行方法とデータ転送の例

CLIサブコマンドは、**環境特化**されており、実行時に必要なファクトリのみを初期化します。

### 1\. GCS環境でのデータ転送 (`gcs-copy`)

GCS URIとローカルファイルのみを扱います。

```bash
# GCSからローカルファイルへの転送
$ go run ./ gcs-copy gs://source-bucket/data.txt -o ./output/local_data.txt

# ローカルからGCSへの転送
$ go run ./ gcs-copy ./local/report.json -o gs://dest-bucket/archive/report.json
```

### 2\. S3環境でのデータ転送 (`s3-copy`)

S3 URIとローカルファイルのみを扱います。

```bash
# S3オブジェクトから標準出力への転送
$ go run ./ s3-copy s3://source-bucket/data.txt

# ローカルファイルからS3バケットへの転送
$ go run ./ s3-copy ./local/image.png -o s3://dest-bucket/archive/image.png --content-type image/png
```

-----

## 📐 ライブラリ構成

CLIサブコマンドの変更に伴い、`cmd`パッケージのファイル名が更新されました。

```
go-remote-io/
├── pkg/
│   ├── remoteio/             # I/Oの核となる機能
│   │   ├── reader.go       # UniversalInputReader (Local / GCS / S3 対応)
│   │   ├── writer.go       # UniversalIOWriter (Local / GCS / S3 対応)
│   │   ├── signer.go       # GCSURLSigner & S3URLSigner の実装
│   │   └── util.go         # URIヘルパー関数 を集約
│   ├── factory/              # GCS専用ファクトリ (GCS環境に特化)
│   │   └── factory.go      # ClientFactory (GCSClientのみを管理)
│   └── s3factory/            # S3専用ファクトリ (AWS環境に特化)
│       └── s3_factory.go    # S3ClientFactory (S3Clientのみを管理)
└── cmd/ 
    ├── root.go             # CLIエントリポイントとファクトリ注入ロジック
    ├── gcs_copy.go         # GCS/ローカルI/O専用コマンド
    └── s3_copy.go          # S3/ローカルI/O専用コマンド
```

### 外部依存パッケージ

本ライブラリは、以下の主要な外部パッケージに依存しています。

* **GCSコア依存**: `cloud.google.com/go/storage`
* **AWSコア依存**: `github.com/aws/aws-sdk-go-v2/...`

-----

### 📜 ライセンス (License)

このプロジェクトは [MIT License](https://opensource.org/licenses/MIT) の下で公開されています。
