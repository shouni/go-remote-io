# 📁 Go Remote IO

[![CI](https://github.com/shouni/go-remote-io/actions/workflows/ci.yml/badge.svg)](https://github.com/shouni/go-remote-io/actions/workflows/ci.yml)
[![Language](https://img.shields.io/badge/Language-Go-blue)](https://golang.org/)
[![Go Version](https://img.shields.io/github/go-mod/go-version/shouni/go-remote-io)](https://golang.org/)
[![GitHub tag (latest by date)](https://img.shields.io/github/v/tag/shouni/go-remote-io)](https://github.com/shouni/go-remote-io/tags)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Reference](https://pkg.go.dev/badge/github.com/shouni/go-remote-io.svg)](https://pkg.go.dev/github.com/shouni/go-remote-io)
[![Status](https://img.shields.io/badge/Status-Active-brightgreen)](#)

## 🚀 概要 (About) - ユニバーサル・クラウドI/O インターフェース

Go Remote IO は、**Google Cloud Storage (GCS)**、**Amazon S3**、および **ローカルファイルシステム**を、統一的なインターフェースで扱うための Go 言語製 I/O ライブラリです。

`gs://`、`s3://`、ローカルパス（`/path/to/...`）のいずれであっても `Open(ctx, path)` 一本で読めるため、アプリケーション側は保存先の違いを意識せず、データの読み書き・一覧取得・リソース管理に集中できます。

-----

## ✨ 提供機能 (Features)

* **スキームに応じた振り分け**: パスの接頭辞（`gs://` / `s3://` / それ以外はローカル）を見て、登録済みのハンドラへ処理を委譲します。**対応スキームは構築時に決まり**、登録されていないスキームは明確に「未対応」として拒否されます。
* **フル機能のリソース操作**: 読み書きだけでなく、**存在確認 (Exists)**、**削除 (Delete)**、**一覧取得 (List)** も同じインターフェースで扱えます。
* **スキームに依らないエラー**: 「見つからない」は必ず `os.ErrNotExist` を包んで返るため、保存先を問わず `errors.Is` 一本で判定できます（[約束事](#-約束事-contracts)）。
* **クラウド SDK 非依存のコア**: `remoteio` パッケージ自体は GCS / AWS の SDK を import しません。抽象だけを使うアプリケーションのビルドにクラウド SDK は入りません。
* **プラグイン可能な実装**: 対応スキームは `SchemeHandler` の集合として `Router` に登録します。独自のバックエンドも同じ口から差し込めます。
* **Functional Options による書き込み制御**: `Content-Type` / `Cache-Control` / `Content-Disposition` を型安全に指定でき、ブラウザでのインライン再生や強制ダウンロードを制御できます。
* **署名付き URL (Signed URL) の生成**: GCS および S3 リソースに対して、期限付きの署名付き URL を生成できます。
* **効率的なリスティング**: 一覧取得はコールバック方式のため、大量のオブジェクトでもメモリ消費を抑えられます。

---

## 🏗 プロジェクトレイアウト (Project Layout)

```text
go-remote-io/
└── remoteio/             # I/Oの核となる抽象化レイヤー（クラウド SDK に依存しない）
    ├── gcs/              # GCS 具象実装 (このパッケージだけが GCS SDK に依存)
    │   ├── factory.go    # GCS クライアントの管理と初期化
    │   ├── handler.go    # 読み込み/一覧/存在確認/書き込み/削除
    │   └── signer.go     # 署名付きURL生成
    ├── s3/               # S3 具象実装 (このパッケージだけが AWS SDK に依存)
    │   ├── factory.go    # S3 クライアントの管理と初期化
    │   ├── handler.go    # 読み込み/一覧/存在確認/書き込み/削除
    │   └── signer.go     # 署名付きURL生成
    ├── interfaces.go     # IOFactory / InputReader / OutputWriter 等の定義
    ├── handler.go        # SchemeHandler (1スキーム分の実装の契約)
    ├── handler_local.go  # ローカルファイルシステムの SchemeHandler 実装
    ├── router.go         # スキームを見て SchemeHandler へ振り分ける Router
    ├── bundle.go         # IOFactory から各コンポーネントを一括で取り出す Bundle
    ├── list_options.go   # ListOption / ListSettings (WithDelimiter 等)
    ├── write_options.go  # WriteOption / WriteSettings (WithContentType 等)
    └── uri.go            # URIの判定・解析ユーティリティ
```

---

## 📖 使い方 (Usage)

### ファクトリから一括で取り出す (`Bundle`)

通常はこの形だけで足ります。`IOFactory` から `InputReader` / `OutputWriter` / `URLSigner` を個別に取り出して構造体へ詰め直す定型処理は、`NewBundle` に集約してあります。

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

stream, err := rio.Reader.Open(ctx, "gs://bucket/object")
```

各アクセサは生成済みのクライアントを包むだけで接続も I/O も伴わないため、使わないコンポーネントが含まれてもコストにはなりません。`Close` は nil レシーバーと nil の `Factory` を許容するので、`[]io.Closer` へまとめて入れる使い方でも安全です。

### スキームを明示的に組み立てる (`Router`)

`gcs.New` / `s3.New` が返すファクトリは、自分のスキームとローカルパスを登録した `Router` を内部で組み立てます。扱うスキームを限定したい場合や、複数のバックエンドを 1 つのリーダーにまとめたい場合だけ直接組み立てます。

```go
// gs:// と s3:// とローカルパスをすべて 1 つのリーダーで扱う
router := remoteio.NewRouter(
    gcs.NewHandler(gcsClient),
    s3.NewHandler(s3Client),
    remoteio.NewLocalHandler(),
)
```

`Router` は `InputReader` と `OutputWriter` の両方を満たします。対応スキームをハンドラの集合という明示的なデータとして持つため、未対応の判定は 1 箇所に集まります。登録済みのスキームは `Router.Schemes()` で確認できます。

### 独自スキームを足す (`SchemeHandler`)

`SchemeHandler` を実装して `NewRouter` に渡すだけです。`Scheme()` が返す接頭辞（`"gs://"` など）がそのまま振り分けのキーになり、空文字を返すハンドラはどのスキームにも一致しなかったパスを受け取るフォールバックになります（ローカルハンドラがこれにあたります）。

```go
type SchemeHandler interface {
    Scheme() string

    Open(ctx context.Context, path string) (io.ReadCloser, error)
    List(ctx context.Context, path string, callback func(string) error, settings ListSettings) error
    Exists(ctx context.Context, path string) (bool, error)
    Write(ctx context.Context, path string, contentReader io.Reader, settings WriteSettings) error
    Delete(ctx context.Context, path string) error
}
```

ハンドラは解決済みの `ListSettings` / `WriteSettings` を受け取ります。オプションの解釈が実装ごとにずれないよう、設定型は公開してあります。`Lister` だけを自前で実装する場合（テストのフェイクを含む）は、受け取った `opts` を `remoteio.NewListSettings(opts...)` で解決すれば本体と同じ設定が得られます。

ハンドラにはパス全体がそのまま渡ります。`gs://` / `s3://` なら `remoteio.ParseRemoteURI` でバケット名とオブジェクト名に分解でき、`remoteio.SchemePrefix` で接頭辞だけを取り出せます。独自スキームの解析はハンドラ自身の責務です。

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

これらを組み合わせた **`InputReader`** (Reader + Lister + Exister) および **`OutputWriter`** (Writer + Remover) を通じて、高レベルな操作を実現します。`IOFactory` はこの 2 つと `URLSigner`、そして `Close` を提供する入口です。

URI の判定・組み立てには `IsGCSURI` / `IsS3URI` / `IsRemoteURI` / `ParseRemoteURI` / `BuildGCSURI` / `BuildS3URI` / `SchemePrefix` を用意しています（`SchemePrefix` 以外は `gs://` と `s3://` 向けのヘルパーです）。

設定から読んだバケット**名**は `NormalizeBucketName` を通してから `BuildGCSURI` / `BuildS3URI` へ渡してください。これらは受け取った値をそのまま連結するため、コンソールから貼った `gs://my-bucket/` を素通しすると `gs://gs://my-bucket//path` という URI ができ、失敗するのは書き込みの時点になります。

---

## 📏 約束事 (Contracts)

### エラー

スキームを問わず同じ意味で返すことが、この抽象が成立するための条件です。

| 状況 | 返り値 |
| :--- | :--- |
| `Open` で対象が無い | `os.ErrNotExist` を包んだエラー（GCS / S3 / ローカルとも `errors.Is` で判定可能） |
| `Exists` で対象が無い | `(false, nil)`。エラーにはしません |
| `Delete` で対象が無い | `nil`。削除は冪等です |
| オブジェクト名が空の URI（`gs://bucket`） | エラー。バケット操作との取り違えを防ぎ、不在と URI 不正を区別するためです |
| 未登録スキーム | 「未対応のURIスキームです」。設定漏れと非対応を区別できます |

### 一覧取得 (`List`) の意味論

区切り文字を渡すかどうかで対象範囲が変わります。

| | 対象 | 疑似ディレクトリ |
| :--- | :--- | :--- |
| `WithDelimiter("/")` あり | prefix の**直下のみ** | 区切り文字で終わるパスとして併せて列挙 |
| なし（既定） | prefix 配下を**再帰的**に、オブジェクト（ファイル）だけ | 列挙しない |

```go
// prefix 直下の疑似ディレクトリ名だけを、配下のオブジェクトを全件走査せずに取得する
err := reader.List(ctx, "gs://bucket/data", func(uri string) error {
    // gs://bucket/data/2026-05-01/  ← 疑似ディレクトリ（末尾が "/"）
    // gs://bucket/data/README.md    ← 直下のオブジェクト
    if strings.HasSuffix(uri, "/") {
        keys = append(keys, path.Base(strings.TrimSuffix(uri, "/")))
    }
    return nil
}, remoteio.WithDelimiter("/"))
```

1 つの疑似ディレクトリに複数のオブジェクトがあるレイアウトでは、区切り文字なしの一覧はその数だけ返るため、呼び出し側で重複を潰すことになります。`WithDelimiter` はその走査をサーバー側へ寄せます。

**prefix の解釈に注意点が 2 つあります。**

* **区切り文字なしの prefix は素の文字列前方一致です。** GCS / S3 の意味論そのままなので、`data` は `data-archive/` にも一致します。ディレクトリとして扱いたい場合は末尾に区切り文字を付けてください（`data/`）。
* **区切り文字ありのときだけ prefix が正規化されます。** `data` は `data/` に補われます（`remoteio.ListPrefix` で再現可能）。補わないと、ディレクトリの中身を見る操作のはずが `data-archive/` まで拾ってしまうためです。

ローカルパスはファイルシステムの性質上ディレクトリ単位の走査になるため、前方一致の挙動だけは一致しません。

### そのほか

* ローカルへの書き込みでは `Content-Type` などのメタデータは保存されず、黙って無視されます。
* ローカルへの書き込みは `context` のキャンセルを検知して打ち切ります（`os.File` への `io.Copy` は本来 ctx を見ません）。
* 署名付き URL はスキーム厳格です。`gcs.NewURLSigner` は `gs://` 以外、`s3.NewURLSigner` は `s3://` 以外を拒否します。S3 は GET / PUT のみ対応です。
* `IOFactory.Close` は冪等で、`Close` 後のアクセサはエラーを返します。
* S3 のリージョンが未設定の場合は `ap-northeast-1` を既定値として使います。

---

## 🔄 v1.7.x からの変更点

`remoteio` パッケージからクラウド SDK 依存を外すため、具象実装を各スキームパッケージへ移しました。それに伴い、移動した型のコンストラクタを削除しています。

| v1.7.x | 現行 |
| :--- | :--- |
| `remoteio.NewUniversalInputReader(gcsClient, s3Client)` | `remoteio.NewRouter(gcs.NewHandler(c), remoteio.NewLocalHandler())` |
| `remoteio.NewUniversalIOWriter(gcsClient, s3Client)` | 同上（`Router` が読み書き両方を満たします） |
| `remoteio.NewGCSURLSigner(client)` | `gcs.NewURLSigner(client)` |
| `remoteio.NewS3URLSigner(client)` | `s3.NewURLSigner(client)` |

`IOFactory` / `InputReader` / `OutputWriter` / `URLSigner` の各インターフェース、`gcs.New` / `s3.New`、Functional Options、URI ユーティリティは変更ありません。**ファクトリ経由で使っている限り、呼び出し側の変更は不要です。**

挙動の変更が 2 点あります。

* 未登録スキームのエラーが「クライアントが未初期化です」から「未対応のURIスキームです」になりました。
* 区切り文字なしのローカル一覧が、直下のみから**再帰的**に変わりました（GCS / S3 と意味を揃えるため）。

---

## 🛠️ 主要な依存関係 (Dependencies)

| サービス | パッケージ / リンク | 説明 |
| :--- | :--- | :--- |
| **GCS** | [cloud.google.com/go/storage](https://github.com/googleapis/google-cloud-go/tree/main/storage) | Google Cloud Storage 公式 Go クライアント（`remoteio/gcs` のみ） |
| **AWS S3** | [aws-sdk-go-v2](https://github.com/aws/aws-sdk-go-v2) | AWS SDK for Go v2（`remoteio/s3` のみ） |

テスト専用の依存として [testify](https://github.com/stretchr/testify)、および実 I/O を伴う統合テスト用に [fake-gcs-server](https://github.com/fsouza/fake-gcs-server) と [gofakes3](https://github.com/johannesboyne/gofakes3) を使用しています。どちらもプロセス内で起動するため、テストの実行に docker や認証情報は要りません。

---

## 📜 ライセンス (License)

このプロジェクトは [MIT License](https://opensource.org/licenses/MIT) の下で公開されています。
