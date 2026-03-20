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
* **メモリ効率に優れたリスティング**: `List` メソッドは**コールバック方式**（ストリーミング）を採用しています。数百万のオブジェクトが存在するバケットでも、メモリを圧迫せずに一覧処理やフィルタリングが可能です。

---

## 📐 ライブラリ構成

```text
go-remote-io
└── remoteio/             # I/Oの核となる機能（インターフェース + Universal実装）
    ├── gcs/              # GCS専用実装
    │   └── factory.go    # GCSクライアントの初期化・提供
    ├── s3/               # S3専用実装
    │   └── factory.go    # S3クライアントの初期化・提供
    ├── interfaces.go     # IOFactory / InputReader / OutputWriter の定義
    ├── reader.go         # UniversalInputReader (GCS/S3/Local を透過的に読み込み)
    ├── writer.go         # UniversalIOWriter (書き込み処理)
    ├── signer.go         # URLSigner (署名付きURL生成)
    └── util.go           # URI判定・解析（IsGCSURI / IsS3URI 等）
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
