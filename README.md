# 📁 Go Remote IO

[![Language](https://img.shields.io/badge/Language-Go-blue)](https://golang.org/)
[![Go Version](https://img.shields.io/github/go-mod/go-version/shouni/go-remote-io)](https://golang.org/)
[![GitHub tag (latest by date)](https://img.shields.io/github/v/tag/shouni/go-remote-io)](https://github.com/shouni/go-remote-io/tags)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Go Remote IO は、**Google Cloud Storage (GCS)**、**Amazon S3**、および**ローカルファイルシステム**への I/O 操作を統一的に扱うための Go 言語製ライブラリです。

このライブラリは、アプリケーションの I/O 依存性を抽象化し、ビジネスロジックから各クラウドストレージやローカルファイルの判別・接続ロジックを完全に分離します。

---

## ✨ 主要な機能と特徴

* **ユニバーサル I/O**: URIのプレフィックスに応じて、**GCS (`gs://`)**、**Amazon S3 (`s3://`)**、または**ローカルファイルパス**へのI/O処理を**透過的に**切り替えます。
* **リソース管理とDI (`package gcsfactory` / `package s3factory` が担当)**:
    * **`gcsfactory`パッケージ**: **GCSクライアント**のみを初期化・管理し、GCP環境に特化します。
    * **`s3factory`パッケージ**: **S3クライアント**のみを初期化・管理し、AWS環境に特化することで、**認証情報の依存関係を完全に分離**します。
* **統一された入力インターフェース**: `remoteio.InputReader` インターフェースを提供し、URI (例: `gs://`, `s3://`) またはローカルファイルパスのどちらが渡されても、ファクトリを介して透過的に `io.ReadCloser` を開きます。
* **統一された出力インターフェース**: `remoteio.OutputWriter` インターフェースを提供します。このインターフェースは**汎用的な `Write(ctx, uri, reader, contentType)` メソッド**を核とし、GCS/S3/ローカルへの書き込みを**透過的に**処理します。
* **期限付きURLの生成**: `remoteio.URLSigner` インターフェースを提供します。GCS および S3 URIに対して**期限付きの署名付きURL (Signed URL)** を生成できます。
* **関心事の分離**: 外部サービスアクセス (`storage.Client`, `s3.Client`) の初期化は外部のファクトリに依存し、I/Oロジック自体は純粋に `remoteio` パッケージ内で完結します。
* **メモリ効率に優れたリスティング**: `List` メソッドは**コールバック方式（ストリーミング）**を採用しています。数百万のオブジェクトが存在するバケットでも、メモリを圧迫せずに一覧処理やフィルタリングが可能です。

---

## 🛠️ インストールと利用

### 1. ライブラリのインストール

```bash
go get github.com/shouni/go-remote-io

```

### 2. 利用方法（InputReader: 読み込みと列挙）

#### A. GCS環境での利用 (`gcsfactory`パッケージを使用)

```go
package main

import (
    "context"
    "fmt"
    "io"
    "log"

    "github.com/shouni/go-remote-io/pkg/gcsfactory" 
)

func main() {
    ctx := context.Background()

    // 1. GCS専用Factoryの初期化とクローズ
	gcsFactory, err := gcsfactory.NewGCSClientFactory(ctx)
    if err != nil {
        log.Fatalf("Factory初期化失敗: %v", err)
    }
    defer func() {
        if closeErr := gcsFactory.Close(); closeErr != nil {
            log.Printf("警告: Factoryのクローズに失敗しました: %v", closeErr)
        }
    }()
    
    // 2. InputReader の実装を取得
    reader, err := gcsFactory.NewInputReader()
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

#### ファイルを列挙する (`List`)

`List` は指定されたプレフィックスに基づき、見つかったパスごとにコールバックを実行します。

```go
prefix := "s3://my-bucket/logs/2026/"
err := reader.List(ctx, prefix, func(filePath string) error {
    // 条件に合うファイルだけを抽出したり、その場で処理できるのだ
    if strings.HasSuffix(filePath, ".log") {
        fmt.Printf("Found log: %s\n", filePath)
    }
    return nil // nilを返すと次のファイルへ、エラーを返すと処理を中断するのだ
})

```

### 3. 利用方法（OutputWriter: 書き込み）

```go
writer, _ := factory.NewOutputWriter()
content := strings.NewReader("Hello, Remote IO!")

// 書き込み先が Local / GCS / S3 でもシグネチャは変わらないのだ
err := writer.Write(ctx, "gs://my-bucket/hello.txt", content, "text/plain")

```

### 3\. 利用方法（URLSigner の例: 期限付きURLの生成）

GCSとS3のSignerは、それぞれのファクトリから取得します。

```go
package main

import (
    "context"
    "log"
    "time"
    "github.com/shouni/go-remote-io/pkg/gcsfactory"
    "github.com/shouni/go-remote-io/pkg/s3factory"
)

func main() {
    ctx := context.Background()
    expires := 15 * time.Minute
    
    // GCS Signerの取得 (GCS専用ファクトリを使用)
    gcsFactory, err := gcsfactory.NewGCSClientFactory(ctx)
    if err != nil {
        log.Fatalf("GCS Factory初期化失敗: %v", err)
    }
    gcsSigner, err := gcsFactory.NewGCSURLSigner()
    if err != nil {
        log.Fatalf("GCS URLSigner生成失敗: %v", err)
    }

    // S3 Signerの取得 (S3専用ファクトリを使用)
    s3Factory, err := s3factory.NewS3ClientFactory(ctx)
    if err != nil {
        log.Fatalf("S3 Factory初期化失敗: %v", err)
    }
    s3Signer, err := s3Factory.NewS3URLSigner()
    if err != nil {
        log.Fatalf("S3 URLSigner生成失敗: %v", err)
    }

    // GCS 署名付きURLを生成
    gcsSignedURL, err := gcsSigner.GenerateSignedURL(ctx, "gs://my-bucket/report.pdf", "GET", expires)
    if err != nil {
        log.Fatalf("GCS署名付きURL生成失敗: %v", err)
    }
    log.Printf("✅ GCS Signed URL: %s", gcsSignedURL)
    
    // S3 署名付きURLを生成
    s3SignedURL, err := s3Signer.GenerateSignedURL(ctx, "s3://my-bucket/data.csv", "PUT", expires)
    if err != nil {
        log.Fatalf("S3署名付きURL生成失敗: %v", err)
    }
    log.Printf("✅ S3 Signed URL: %s", s3SignedURL)
}
```

---

## 💻 CLI実行方法

CLIサブコマンドは環境ごとに最適化されており、必要な認証情報のみを使用して動作します。

```bash
# GCSとローカル間でのコピー
$ go run ./ gcs-copy gs://source-bucket/image.png -o ./local/image.png

# S3とローカル間でのコピー
$ go run ./ s3-copy ./local/data.csv -o s3://dest-bucket/data.csv --content-type text/csv

```

---

## 📐 ライブラリ構成

```
go-remote-io/
├── pkg/
│   ├── remoteio/        # I/Oの核となる機能
│   │   ├── reader.go    # UniversalInputReader (Open / List)
│   │   ├── writer.go    # UniversalOutputWriter (Write)
│   │   ├── signer.go    # URLSigner の実装
│   │   └── util.go      # URI解析・バリデーション
│   ├── gcsfactory/      # GCS専用ファクトリ
│   └── s3factory/       # S3専用ファクトリ
└── cmd/ 
    ├── root.go          # CLIエントリポイント
    ├── gcs_copy.go      # GCS/ローカル専用コマンド
    └── s3_copy.go       # S3/ローカル専用コマンド

```

### 🛠️ 主要な依存関係 (Dependencies)

| サービス | パッケージ / リンク | 説明 |
| :--- | :--- | :--- |
| **GCS** | [cloud.google.com/go/storage](https://github.com/googleapis/google-cloud-go/tree/main/storage) | Google Cloud Storage 公式 Go クライアント |
| **AWS S3** | [aws-sdk-go-v2](https://github.com/aws/aws-sdk-go-v2) | AWS SDK for Go v2 (S3/Config/Signer) |
| **Testing** | [testify](https://github.com/stretchr/testify) | アサーションおよびモック用テストフレームワーク |

---

### 📜 ライセンス (License)

このプロジェクトは [MIT License](https://opensource.org/licenses/MIT) の下で公開されています。
