# 📁 Go Remote IO

[![Language](https://img.shields.io/badge/Language-Go-blue)](https://golang.org/)
[![Go Version](https://img.shields.io/github/go-mod/go-version/shouni/go-remote-io)](https://golang.org/)
[![GitHub tag (latest by date)](https://img.shields.io/github/v/tag/shouni/go-remote-io)](https://github.com/shouni/go-remote-io/tags)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

## 🚀 概要 (About) - マルチクラウド I/O インターフェース

Go Remote IO は、**Google Cloud Storage (GCS)**、**Amazon S3**、および **ローカルファイルシステム**を、統一的なインターフェースで扱うための Go 言語製 I/O ライブラリです。

`gs://`、`s3://`、ローカルパス（`/path/to/...`）といった **path** に応じて適切なストレージ実装を選択できるため、アプリケーション側は保存先の違いを過度に意識せず、データの読み書きや一覧取得に集中できます。

-----

## ✨ 提供機能 (Features)

* **ユニバーサル I/O**: path に応じて、**GCS**、**S3**、**ローカルファイルシステム**へのアクセスを統一的に扱えます。
* **プラグイン可能な実装**:
  * **GCS サブパッケージ**: Google Cloud Storage 公式クライアントを利用した I/O 実装を提供します。
  * **S3 サブパッケージ**: AWS SDK for Go v2 を利用した S3 向け I/O 実装を提供します。
* **統一されたインターフェース**: 単一リソースの読み込み用 `Reader`、書き込み用 `Writer`、一覧取得を含む `InputReader`、署名付き URL を生成する `URLSigner` を提供します。
* **署名付き URL (Signed URL) の生成**: GCS および S3 リソースに対して、期限付きの署名付き URL を生成できます。
* **効率的なリスティング**: 一覧取得にはコールバック方式を採用しており、大量のオブジェクトに対してもメモリ消費を抑えながら処理できます。
* **Factory ベースの設計**: `ReadWriteFactory` / `IOFactory` を通じて各機能へアクセスできるため、依存関係の差し替えやテスト用モックの導入がしやすい構成です。
* **DI (Dependency Injection) フレンドリー**: ストレージクライアントや実装の生成責務を分離しやすく、環境ごとの設定変更やテストが容易です。

---

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