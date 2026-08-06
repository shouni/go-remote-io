# 📁 Go Remote IO

[![CI](https://github.com/shouni/go-remote-io/actions/workflows/ci.yml/badge.svg)](https://github.com/shouni/go-remote-io/actions/workflows/ci.yml)
[![Language](https://img.shields.io/badge/Language-Go-blue)](https://golang.org/)
[![Go Version](https://img.shields.io/github/go-mod/go-version/shouni/go-remote-io)](https://golang.org/)
[![GitHub tag (latest by date)](https://img.shields.io/github/v/tag/shouni/go-remote-io)](https://github.com/shouni/go-remote-io/tags)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Reference](https://pkg.go.dev/badge/github.com/shouni/go-remote-io.svg)](https://pkg.go.dev/github.com/shouni/go-remote-io)
[![Status](https://img.shields.io/badge/Status-Completed-brightgreen)](#)

## 🚀 概要 (About) - ユニバーサル・クラウドI/O インターフェース

Go Remote IO は、**Google Cloud Storage (GCS)**、**Amazon S3**、および **ローカルファイルシステム**を、統一的なインターフェースで扱うための Go 言語製 I/O ライブラリです。

`gs://`、`s3://`、ローカルパス（`/path/to/...`）といった **path** に応じて適切なストレージ実装を選択できるため、アプリケーション側は保存先の違いを過度に意識せず、データの読み書き、一覧取得、リソースの管理に集中できます。

-----

## ✨ 提供機能 (Features)

* **ユニバーサル I/O**: path に応じて、**GCS**、**S3**、**ローカルファイルシステム**へのアクセスを自動的に振り分けます。
* **フル機能のリソース操作**: 単なる読み書きだけでなく、**リソースの存在確認 (Exists)** や **削除 (Delete)** も統一インターフェースでサポート。
* **Functional Options による書き込み制御**: `Content-Type` や `Content-Disposition` を型安全かつ柔軟に指定可能。ブラウザでのインライン再生や強制ダウンロードの制御が容易です。
* **プラグイン可能な実装**:
  * **GCS サブパッケージ**: Google Cloud Storage 公式クライアントを利用した I/O 実装。
  * **S3 サブパッケージ**: AWS SDK for Go v2 を利用した S3 向け I/O 実装。
* **統一されたインターフェース**:
  * `InputReader`: 読み込み (`Open`)、一覧取得 (`List`)、存在確認 (`Exists`) を統合。
  * `OutputWriter`: 書き込み (`Write`)、削除 (`Delete`) を統合。
* **署名付き URL (Signed URL) の生成**: GCS および S3 リソースに対して、期限付きの署名付き URL を生成できます。
* **効率的なリスティング**: 一覧取得にはコールバック方式を採用しており、大量のオブジェクトに対してもメモリ消費を抑えながら処理できます。

---

## 🏗 プロジェクトレイアウト (Project Layout)

```text
go-remote-io/
└── remoteio/             # I/Oの核となる抽象化レイヤー
    ├── gcs/              # GCS 具象実装
    │   └── factory.go    # GCS クライアントの管理と初期化
    ├── s3/               # S3 具象実装
    │   └── factory.go    # S3 クライアントの管理と初期化
    ├── interfaces.go     # IOFactory / InputReader / OutputWriter 等の定義
    ├── bundle.go         # IOFactory から各コンポーネントを一括で取り出す Bundle
    ├── reader.go         # UniversalInputReader の振り分け
    ├── reader_local.go   # ローカルファイルの読み込み/一覧/存在確認
    ├── reader_gcs.go     # GCS の読み込み/一覧/存在確認
    ├── reader_s3.go      # S3 の読み込み/一覧/存在確認
    ├── write_options.go  # Functional Options (WithContentType, etc.) の定義
    ├── writer.go         # UniversalIOWriter の振り分け
    ├── writer_local.go   # ローカルファイルの書き込み/削除
    ├── writer_gcs.go     # GCS の書き込み/削除
    ├── writer_s3.go      # S3 の書き込み/削除
    ├── signer.go         # URLSigner (署名付きURL生成の抽象化)
    └── uri.go            # URIの判定・解析ユーティリティ
```

---

## 🧩 主要インターフェース (Key Interfaces)

Go Remote IO は、役割ごとに細分化されたインターフェースを提供しています。

| インターフェース | メソッド | 説明 |
| :--- | :--- | :--- |
| **Reader** | `Open` | リソースを `io.ReadCloser` として開く |
| **Writer** | `Write` | リソースへデータを書き込む |
| **Exister** | `Exists` | リソースの存在を確認する |
| **Remover** | `Delete` | リソースを削除する |
| **Lister** | `List` | 指定パス配下のリソースを一覧取得する |

これらを組み合わせた **`InputReader`** (Reader + Lister + Exister) および **`OutputWriter`** (Writer + Remover) を通じて、高レベルな操作を実現します。

#### 一括で取り出す (`Bundle`)

`IOFactory` から `InputReader` / `OutputWriter` / `URLSigner` を個別に取り出して構造体へ詰め直す定型処理は、`NewBundle` に集約してあります。

```go
factory, err := gcs.New(ctx)
if err != nil {
    return err
}

rio, err := remoteio.NewBundle(factory)
if err != nil {
    _ = factory.Close() // 失敗時の factory は呼び出し元が所有したままです
    return err
}
defer func() { _ = rio.Close() }() // 成功後のライフサイクルは Bundle が持ちます

rio.Reader.Open(ctx, "gs://bucket/object")
```

各アクセサは生成済みのクライアントを包むだけで接続も I/O も伴わないため、使わないコンポーネントが含まれてもコストにはなりません。`Close` は nil レシーバーと nil の `Factory` を許容するので、`[]io.Closer` へまとめて入れる使い方でも安全です。

#### 階層を意識した一覧取得 (`WithDelimiter`)

`List` に `WithDelimiter("/")` を渡すと、指定プレフィックスの**直下だけ**が対象になり、
「疑似ディレクトリ」が区切り文字で終わる URI として併せて列挙されます。

```go
// gs://bucket/music/ 配下のジョブ ID を、成果物を全件走査せずに取得する
err := reader.List(ctx, "gs://bucket/music", func(uri string) error {
    // gs://bucket/music/20260501-abcd/  ← 疑似ディレクトリ（末尾が "/"）
    // gs://bucket/music/README.md       ← 直下のオブジェクト
    if strings.HasSuffix(uri, "/") {
        jobIDs = append(jobIDs, path.Base(strings.TrimSuffix(uri, "/")))
    }
    return nil
}, remoteio.WithDelimiter("/"))
```

1 ジョブが複数の成果物を持つレイアウトでは、区切り文字なしの一覧はその数だけオブジェクトを
返すため、呼び出し側で重複を潰すことになります。`WithDelimiter` はその走査をサーバー側へ寄せます。

プレフィックスに区切り文字が無い場合は自動で補われます（`music` → `music/`）。
補わないと `music-archive/` まで一致してしまうためです。

`Lister` を自前で実装する場合（テストのフェイクを含む）は、受け取った `opts` を
`remoteio.NewListSettings(opts...)` で解決すると本体と同じ設定が得られます。
プレフィックスの正規化も `remoteio.ListPrefix` で再現できます。

ローカルパスに対しても同じ意味で働き、区切り文字を指定したときだけディレクトリが列挙されます
（指定しない場合の挙動は従来どおりファイルのみです）。

---

### 🛠️ 主要な依存関係 (Dependencies)

| サービス | パッケージ / リンク | 説明 |
| :--- | :--- | :--- |
| **GCS** | [cloud.google.com/go/storage](https://github.com/googleapis/google-cloud-go/tree/main/storage) | Google Cloud Storage 公式 Go クライアント |
| **AWS S3** | [aws-sdk-go-v2](https://github.com/aws/aws-sdk-go-v2) | AWS SDK for Go v2 |
| **Testing** | [testify](https://github.com/stretchr/testify) | アサーションおよびモック用テストフレームワーク |

---

### 📜 ライセンス (License)

このプロジェクトは [MIT License](https://opensource.org/licenses/MIT) の下で公開されています。
