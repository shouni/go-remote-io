# 📁 Go Remote IO

[![Language](https://img.shields.io/badge/Language-Go-blue)](https://golang.org/)
[![Go Version](https://img.shields.io/github/go-mod/go-version/shouni/go-remote-io)](https://golang.org/)
[![GitHub tag (latest by date)](https://img.shields.io/github/v/tag/shouni/go-remote-io)](https://github.com/shouni/go-remote-io/tags)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

## 🚀 概要 (About) - 両翼を統べる、透過的マルチクラウドインターフェース

Go Remote IO は、**Google Cloud Storage (GCS)**、**Amazon S3**、および**ローカルファイルシステム**を透過的に扱うための抽象化 I/O ライブラリです。

URI プロトコル（`gs://`, `s3://`, `/path/to/`）に基づいて最適なストレージ実装を自動的に選択するため、ビジネスロジックは「どこに保存されているか」の詳細を意識することなく、純粋なデータ処理に集中できます。

-----

## ✨ 提供機能 (Features)

* **ユニバーサル I/O**: URI のプレフィックスに応じて、**GCS**、**S3**、**ローカルファイル**へのアクセスを**透過的に**切り替えます。
* **プラグイン可能な実装 (`remoteio/gcs`, `remoteio/s3`)**:
  * **GCS サブパッケージ**: Google Cloud Storage 公式クライアントを用いた高機能な I/O 実装を提供します。
  * **S3 サブパッケージ**: AWS SDK v2 に基づく S3 実装を提供。認証情報の管理をパッケージ単位で分離し、不要な依存関係の混入を防ぎます。
* **統一された Reader / Writer インターフェース**: URI が渡されるだけで適切な `io.ReadCloser` や書き込みストリームを生成する、シンプルで強力な `InputReader` と `OutputWriter` を提供します。
* **署名付き URL (Signed URL) の生成**: GCS および S3 リソースに対して、期限付きの署名付き URL を生成する `URLSigner` インターフェースをサポート。
* **効率的なリスティング**: オブジェクトの一覧取得には**コールバック（ストリーミング）方式**を採用。数百万規模のオブジェクトが存在するバケットでも、メモリ消費を最小限に抑えた処理が可能です。
* **DI (Dependency Injection) フレンドリー**: 各ストレージクライアントの初期化は外部のファクトリに委ねる設計になっており、テスト時のモック化や環境ごとの設定変更が容易です。

-----

## 🏗 プロジェクトレイアウト (Project Layout)

```text
go-remote-io/
└── remoteio/             # I/Oの核となる抽象化レイヤー
    ├── gcs/              # GCS 具象実装（旧 gcsfactory）
    │   └── factory.go    # GCS クライアントの管理と初期化
    ├── s3/               # S3 具象実装（旧 s3factory）
    │   └── factory.go    # S3 クライアントの管理と初期化
    ├── interfaces.go     # IOFactory / InputReader / OutputWriter 等の定義
    ├── reader.go         # UniversalInputReader (マルチプロトコル読み込み)
    ├── writer.go         # UniversalIOWriter (マルチプロトコル書き込み)
    ├── signer.go         # URLSigner (署名付きURL生成の抽象化)
    └── util.go           # URIの判定・解析ユーティリティ
```

### 🛠️ 主要な依存関係 (Dependencies)

| サービス | パッケージ / リンク | 説明 |
| :--- | :--- | :--- |
| **GCS** | [cloud.google.com/go/storage](https://github.com/googleapis/google-cloud-go/tree/main/storage) | Google Cloud Storage 公式 Go クライアント |
| **AWS S3** | [aws-sdk-go-v2](https://github.com/aws/aws-sdk-go-v2) | AWS SDK for Go v2 |
| **Testing** | [testify](https://github.com/stretchr/testify) | アサーションおよびモック用テストフレームワーク |

---

### 📜 ライセンス (License)

このプロジェクトは [MIT License](https://opensource.org/licenses/MIT) の下で公開されています。

---