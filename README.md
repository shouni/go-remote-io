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

`gs://`、`s3://`、`file://`、ローカルパス（`/path/to/...`）のいずれであっても `Open(ctx, path)` 一本で読めるため、アプリケーション側は保存先の違いを意識せず、データの読み書き・一覧取得・リソース管理に集中できます。

-----

## ✨ 提供機能 (Features)

* **スキームに応じた振り分け**: パスの接頭辞（`gs://` / `s3://` / `file://` / それ以外はローカル）を見て、登録済みのハンドラへ処理を委譲します。**対応スキームは構築時に決まり**、登録されていないスキームは明確に「未対応」として拒否されます。
* **フル機能のリソース操作**: 読み書きだけでなく、**存在確認 (Exists)**、**メタデータ取得 (Stat)**、**削除 (Delete)**、**一覧取得 (List)** も同じインターフェースで扱えます。
* **スキームを跨ぐコピー**: `Copy` / `Move` は `gs://` から `s3://` へも、リモートからローカルへも同じ呼び出しです。中身をメモリに読み切らずストリームで渡します。
* **スキームに依らないエラー**: 「見つからない」は必ず `os.ErrNotExist` を包んで返るため、保存先を問わず `errors.Is` 一本で判定できます（[約束事](#-約束事-contracts)）。
* **クラウド SDK 非依存のコア**: `remoteio` パッケージ自体は GCS / AWS の SDK を import しません。抽象だけを使うアプリケーションのビルドにクラウド SDK は入りません。
* **プラグイン可能な実装**: 対応スキームは `SchemeHandler` の集合として `Router` に登録します。独自のバックエンドも同じ口から差し込めます。
* **Functional Options による書き込み制御**: `Content-Type` / `Cache-Control` / `Content-Disposition` を型安全に指定でき、ブラウザでのインライン再生や強制ダウンロードを制御できます。
* **署名付き URL (Signed URL) の生成**: GCS および S3 リソースに対して、期限付きの署名付き URL を生成できます。
* **効率的なリスティング**: 一覧取得はコールバック方式のため、大量のオブジェクトでもメモリ消費を抑えられます。`remoteio.Files` を使えば `for range` で回すこともできます。
* **接続先の差し替え**: `s3.WithEndpoint` / `s3.WithPathStyle` で MinIO や Cloudflare R2 のような S3 互換ストレージへ、`gcs.WithClient` / `s3.WithClient` で生成済みクライアントへ接続できます。
* **複数クラウドの同時利用**: `remoteio.NewMultiFactory` は複数の `IOFactory` を 1 つに束ね、`gs://` と `s3://` を単一の `Bundle` で扱えるようにします。
* **壊れかけのファイルを残さないローカル書き込み**: 一時ファイルへ書いてから `rename` するため、キャンセルや I/O エラーで中断しても既存ファイルは壊れません。

---

## 🏗 プロジェクトレイアウト (Project Layout)

```text
go-remote-io/
└── remoteio/             # I/Oの核となる抽象化レイヤー（クラウド SDK に依存しない）
    ├── gcs/              # GCS 具象実装 (このパッケージだけが GCS SDK に依存)
    │   ├── factory.go    # GCS クライアントの管理と初期化 (Option でクライアント注入)
    │   ├── handler.go    # 読み込み/一覧/存在確認/メタデータ/書き込み/削除
    │   └── signer.go     # 署名付きURL生成
    ├── s3/               # S3 具象実装 (このパッケージだけが AWS SDK に依存)
    │   ├── factory.go    # S3 クライアントの管理と初期化 (Option でエンドポイント等を指定)
    │   ├── handler.go    # 読み込み/一覧/存在確認/メタデータ/書き込み/削除
    │   └── signer.go     # 署名付きURL生成
    ├── interfaces.go     # IOFactory / InputReader / OutputWriter / Stater 等の定義
    ├── handler.go        # SchemeHandler (1スキーム分の実装の契約)
    ├── handler_local.go  # ローカルファイルシステムの SchemeHandler 実装
    ├── handler_file.go   # file:// をローカルパスへ読み替える SchemeHandler
    ├── router.go         # スキームを見て SchemeHandler へ振り分ける Router
    ├── factory.go        # NewSchemeRouter (リモート + ローカル + file:// の組み立て)
    ├── multi.go          # 複数の IOFactory を 1 つに束ねる MultiFactory
    ├── bundle.go         # IOFactory から各コンポーネントを一括で取り出す Bundle
    ├── ops.go            # Copy / Move / ReadAll / Files / Stat の補助関数
    ├── list_options.go   # ListOption / ListSettings (WithDelimiter 等)
    ├── write_options.go  # WriteOption / WriteSettings (WithContentType 等)
    ├── path.go           # ベースディレクトリ・パス結合のユーティリティ
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

### 接続先を指定する (`Option`)

`gcs.New` / `s3.New` は Functional Options を受け取ります。オプションを渡さない場合の挙動（GCS は Application Default Credentials、S3 は IAM ロールや環境変数からの自動検索）は変わりません。

```go
// MinIO / Cloudflare R2 などの S3 互換ストレージ
factory, err := s3.New(ctx,
    s3.WithEndpoint("http://localhost:9000"),
    s3.WithPathStyle(), // 仮想ホスト形式のバケット名を解決できない実装向け
    s3.WithRegion("ap-northeast-1"),
)

// 生成済みのクライアントを再利用する（テストのフェイク接続など）
factory, err := gcs.New(ctx, gcs.WithClient(storageClient))
```

| パッケージ | オプション |
| :--- | :--- |
| `gcs` | `WithClient` / `WithClientOptions` |
| `s3` | `WithClient` / `WithConfig` / `WithRegion` / `WithEndpoint` / `WithPathStyle` / `WithConfigOptions` / `WithS3Options` |

`WithClient` で注入したクライアントのライフサイクルは**呼び出し元に残ります**（`Close` は閉じません）。閉じる主体が 2 つあると、どちらが所有しているのか呼び出し側から分からなくなるためです。

### 複数のクラウドを 1 つの Bundle で扱う (`MultiFactory`)

`gs://` と `s3://` を同時に扱いたい場合は、それぞれのファクトリを束ねます。署名付き URL もスキームを見て振り分けられます。

```go
gcsFactory, err := gcs.New(ctx)
s3Factory, err := s3.New(ctx)

multi, err := remoteio.NewMultiFactory(gcsFactory, s3Factory)
if err != nil {
    // 失敗時はどの factory も閉じずに返るため、呼び出し元が後始末できます
    return err
}

rio, err := remoteio.NewBundle(multi)
defer func() { _ = rio.Close() }() // 束ねた factory がまとめて閉じます

_ = rio.Writer.Write(ctx, "gs://bucket/a.txt", src)
_ = rio.Writer.Write(ctx, "s3://bucket/a.txt", src)
```

### コピー・一覧・メタデータ

```go
// スキームを跨いだコピー（中身はメモリに読み切らずストリームで渡ります）
err := remoteio.Copy(ctx, rio.Reader, rio.Writer, "gs://bucket/a.txt", "s3://bucket/a.txt")

// コピーが成功したときだけコピー元を消す
err = remoteio.Move(ctx, rio.Reader, rio.Writer, src, dst)

// サイズ・更新時刻・Content-Type・ユーザー定義メタデータ
info, err := remoteio.Stat(ctx, rio.Reader, "gs://bucket/a.txt")

// イテレータ版の一覧（break で抜けると List 側も打ち切られます）
for path, err := range remoteio.Files(ctx, rio.Reader, "gs://bucket/data") {
    if err != nil {
        return err
    }
    fmt.Println(path)
}
```

### スキームを明示的に組み立てる (`Router`)

`gcs.New` / `s3.New` が返すファクトリは、自分のスキームとローカル関連のパスを登録した `Router` を内部で組み立てます。扱うスキームを限定したい場合だけ直接組み立てます。

```go
// gs:// と s3:// とローカルパスをすべて 1 つのリーダーで扱う
router := remoteio.NewRouter(
    gcs.NewHandler(gcsClient),
    s3.NewHandler(s3Client),
    remoteio.NewLocalHandler(),
)

// ローカル関連（スキームなし + file://）を自動で足す版
router = remoteio.NewSchemeRouter(gcs.NewHandler(gcsClient))
```

`Router` は `InputReader` と `OutputWriter` の両方を満たします。対応スキームをハンドラの集合という明示的なデータとして持つため、未対応の判定は 1 箇所に集まります。登録済みのスキームは `Router.Schemes()` で確認できます（辞書順）。

### 独自スキームを足す (`SchemeHandler`)

`SchemeHandler` を実装して `NewRouter` に渡すだけです。`Scheme()` が返す接頭辞（`"gs://"` など）がそのまま振り分けのキーになり、空文字を返すハンドラはどのスキームにも一致しなかったパスを受け取るフォールバックになります（ローカルハンドラがこれにあたります）。

```go
type SchemeHandler interface {
    Scheme() string

    Open(ctx context.Context, path string) (io.ReadCloser, error)
    Stat(ctx context.Context, path string) (ObjectInfo, error)
    List(ctx context.Context, path string, callback func(string) error, settings ListSettings) error
    Exists(ctx context.Context, path string) (bool, error)
    Write(ctx context.Context, path string, contentReader io.Reader, settings WriteSettings) error
    Delete(ctx context.Context, path string) error
}
```

ハンドラは解決済みの `ListSettings` / `WriteSettings` を受け取ります。オプションの解釈が実装ごとにずれないよう、設定型は公開してあります。`Lister` だけを自前で実装する場合（テストのフェイクを含む）は、受け取った `opts` を `remoteio.NewListSettings(opts...)` で解決すれば本体と同じ設定が得られます。

ハンドラにはパス全体がそのまま渡ります。「スキーム + バケット + キー」形式なら、スキームを問わず `remoteio.ParseBucketURI` で分解できます。担当スキームであることを併せて検証したい場合は `remoteio.ParseSchemeURI`（キーは空でも可）または `remoteio.ParseSchemeObjectURI`（キー必須）を使ってください。組み立ては `remoteio.BuildURI` です。

---

## 🧩 主要インターフェース (Key Interfaces)

Go Remote IO は、役割ごとに細分化されたインターフェースを提供しています。

| インターフェース | メソッド | 説明 |
| :--- | :--- | :--- |
| **Reader** | `Open` | リソースを `io.ReadCloser` として開く |
| **Writer** | `Write` | リソースへデータを書き込む |
| **Exister** | `Exists` | リソースの存在を確認する |
| **Stater** | `Stat` | サイズ・更新時刻・Content-Type・メタデータを取得する |
| **Remover** | `Delete` | リソースを削除する |
| **Lister** | `List` | 指定パス配下のリソースを一覧取得する |

これらを組み合わせた **`InputReader`** (Reader + Lister + Exister) および **`OutputWriter`** (Writer + Remover) を通じて、高レベルな操作を実現します。`IOFactory` はこの 2 つと `URLSigner`、そして `Close` を提供する入口です。

`Stater` は `InputReader` に**含めていません**。含めると、この複合インターフェースを実装している既存のフェイクや代替実装が一斉にコンパイルできなくなるためです。`*Router` は `Stater` を満たすので、`remoteio.Stat(ctx, reader, path)` から使えます（対応していない実装には型が分かるエラーを返します）。

URI の判定・組み立てには、スキームを問わない `ParseBucketURI` / `BuildURI` / `ParseSchemeURI` / `ParseSchemeObjectURI` / `SchemePrefix` と、`gs://` / `s3://` 向けの `IsGCSURI` / `IsS3URI` / `IsRemoteURI` / `ParseRemoteURI` / `BuildGCSURI` / `BuildS3URI` を用意しています。

パスの操作には `ResolveBaseDir`（親ディレクトリ）、`ResolvePath`（結合）、`GenerateIndexedPath`（拡張子の前に連番を挿入）があります。リモート URI は URL ではなく「スキーム + バケット + 生のキー」として扱うため、オブジェクト名に含まれる空白や `?` はエンコードされません。

設定から読んだバケット**名**は `NormalizeBucketName` を通してから `BuildGCSURI` / `BuildS3URI` へ渡してください。これらは受け取った値をそのまま連結するため、コンソールから貼った `gs://my-bucket/` を素通しすると `gs://gs://my-bucket//path` という URI ができ、失敗するのは書き込みの時点になります。

---

## 📏 約束事 (Contracts)

### エラー

スキームを問わず同じ意味で返すことが、この抽象が成立するための条件です。

| 状況 | 返り値 |
| :--- | :--- |
| `Open` / `Stat` で対象が無い | `os.ErrNotExist` を包んだエラー（GCS / S3 / ローカルとも `errors.Is` で判定可能） |
| `Exists` で対象が無い | `(false, nil)`。エラーにはしません |
| `Delete` で対象が無い | `nil`。削除は冪等です |
| オブジェクト名が空の URI（`gs://bucket`） | エラー。バケット操作との取り違えを防ぎ、不在と URI 不正を区別するためです |
| 未登録スキーム | 「未対応のURIスキームです」。設定漏れと非対応を区別できます |
| ハンドラへ担当外のスキームを直接渡した | 「スキームが一致しません」。`Router` 経由なら振り分けの時点で弾かれます |

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

* ローカルへの書き込みでは `Content-Type` やユーザー定義メタデータは保存されず、黙って無視されます。`Stat` の `ContentType` は空、`Metadata` は nil になります。
* ローカルへの書き込みは `context` のキャンセルを検知して打ち切ります（`os.File` への `io.Copy` は本来 ctx を見ません）。
* ローカルへの書き込みは一時ファイルへ書いてから `rename` します。キャンセルや I/O エラーで中断しても、既存ファイルは書き換わらず、新規ファイルも残りません。書き込まれたファイルのパーミッションは `0644` です。
* `file://` はローカルパスへ読み替えて扱います。`file:///tmp/a.txt` は `/tmp/a.txt` です。URI の規約どおりパーセントデコードするため、名前に `%` を含むファイルは `%25` と書く必要があります。一覧結果も `file://` 付きで返ります。
* S3 はユーザー定義メタデータのキーを小文字へ正規化するため、`Stat` で読み戻したときに大文字小文字が変わることがあります。
* 署名付き URL はスキーム厳格です。`gcs.NewURLSigner` は `gs://` 以外、`s3.NewURLSigner` は `s3://` 以外を拒否します。S3 は GET / PUT のみ対応です。
* `IOFactory.Close` は冪等で、`Close` 後のアクセサはエラーを返します。
* S3 のリージョンが未設定の場合は `ap-northeast-1`（`s3.DefaultRegion`）を既定値として使います。優先順は `WithRegion` > 環境変数や設定ファイル > 既定値です。

---

## 🔄 v1.8.0 での変更点

追加が中心ですが、**互換性に影響する変更が 3 点**あります。

| 変更 | 影響 |
| :--- | :--- |
| `SchemeHandler` に `Stat` を追加 | **独自の `SchemeHandler` 実装は `Stat` の追加が必要**です。`InputReader` / `OutputWriter` は変更していないため、そちらを実装しているフェイクや代替実装は影響を受けません |
| `ResolveBaseDir` / `ResolvePath` がパーセントエンコードをやめた | オブジェクト名に空白や `?` を含むパスの結果が変わります。以前は `my dir/a.txt` が `my%20dir/a.txt` になり、エラーにならないまま別のキーを指していました |
| ハンドラが担当外のスキームを拒否 | `gcs.NewHandler(...).Open(ctx, "s3://...")` のような直接呼び出しがエラーになります。`Router` 経由では以前から弾かれていました |

そのほかの修正:

* `gcs.Handler.List` が nil クライアントで panic していたのを、他の操作と同じくエラーに揃えました。
* `ResolveBaseDir` が URL のパスに `filepath.Dir` を使っていたため、Windows でセパレータが混ざっていたのを直しました。

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
