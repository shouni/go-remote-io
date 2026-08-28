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

`gs://`、`s3://`、`file://`、ローカルパス（`/path/to/...`）のいずれであっても `Open(ctx, name)` 一本で読めるため、アプリケーション側は保存先の違いを意識せず、データの読み書き・一覧取得・リソース管理に集中できます。

```go
factory, err := gcs.New(ctx)
if err != nil {
    return err
}
defer func() { _ = factory.Close() }()

store, err := factory.Store()
if err != nil {
    return err
}

// 組み立て時に一度だけスコープを決めれば、以降はバケットを連れ回さずに済みます
jobs := store.Sub("gs://my-bucket/jobs")
err = jobs.Write(ctx, jobID+"/status.json", body, remoteio.WithContentType("application/json"))
```

-----

## ✨ 提供機能 (Features)

* **スキームに応じた振り分け**: `gs://` / `s3://` / `file://` / それ以外はローカル、を見て登録済みのハンドラへ委譲します。**対応スキームは構築時に決まり**、登録されていないスキームは `ErrUnsupportedScheme` で明確に拒否されます。
* **1 つの窓口 (`Store`)**: 読み書き・一覧・存在確認・メタデータ・削除・コピー・署名付き URL を 1 つのインターフェースが持ちます。依存を絞りたい関数は `Reader`（`Open` だけ）や `Writer`（`Write` だけ）を受け取れます。
* **スコープ付きストア (`Sub`)**: プレフィックスに固定したストアを作れます。呼び出しのたびにバケット名を連れ回す必要がなくなります。
* **原子的な書き込み**: 成功しなければ書き込み先は変化しません。ローカルは一時ファイル + `rename`、GCS はアップロードの中断、S3 はマルチパートの abort で実現しています。条件付き書き込み (`WithIfNotExists`) も使えます。
* **型のついた一覧**: `List` は `iter.Seq2[Entry, error]` を返し、疑似ディレクトリは `Entry.IsPrefix` で分かります。`break` でそのまま打ち切れます。
* **サーバーサイドコピー**: `Store.Copy` は、同じスキーム内でハンドラが対応していればサーバー側のコピーへ落とし、そうでなければストリームで中継します。呼び出し側に分岐は要りません。
* **スキームに依らないエラー**: 「見つからない」は必ず `ErrNotExist`（`io/fs` と同じ値）を包んで返るため、保存先を問わず `errors.Is` 一本で判定できます。
* **クラウド SDK 非依存のコア**: `remoteio` パッケージ自体は GCS / AWS の SDK を import しません。抽象だけを使うアプリケーションのビルドにクラウド SDK は入りません。
* **テスト用のインメモリ実装**: `memio` が本物と同じ契約を満たすハンドラを提供します。手書きのフェイクは要りません。
* **適合性テストスイート**: `storetest.TestHandler` が、GCS / S3 / ローカル / `memio` / 第三者の実装を同じ 1 本のスイートで検査します。
* **接続先の差し替え**: `s3.WithEndpoint` / `s3.WithPathStyle` で MinIO や Cloudflare R2 のような S3 互換ストレージへ、`gcs.WithClient` / `s3.WithClient` で生成済みクライアントへ接続できます。
* **`io/fs` との相互運用**: `remoteio.FS(ctx, store)` で読み取り専用の `fs.FS` として渡せます。

---

## 🏗 パッケージ構成 (Packages)

| パッケージ | 持つもの |
| :--- | :--- |
| `remoteio` | 抽象（`Store` / `Handler` / `Entry` / エラーの語彙 / URI とパスの操作）とローカル・`file://` の実装 |
| `remoteio/gcs` | GCS 具象実装。クライアントの寿命と `Handler` / `Copier` / `Signer` |
| `remoteio/s3` | S3 具象実装。同上 |
| `remoteio/memio` | インメモリの `Handler`。テストで本物の代わりに使う |
| `remoteio/storetest` | `Handler` の適合性テストスイート |

クラウド SDK に触れてよいのは `gcs` と `s3` だけです。抽象だけを使うアプリケーションのビルドに SDK を持ち込まないための境界です。

---

## 📖 使い方 (Usage)

### スコープを絞る (`Sub`)

`Sub` が返すストアはプレフィックスに固定され、そこからの**相対名だけ**を受け取ります。

```go
jobs := store.Sub("gs://my-bucket/jobs")

_ = jobs.Write(ctx, "j1/status.json", body)
data, _ := remoteio.ReadAll(ctx, jobs, "j1/status.json")

// さらに絞れます
j1 := jobs.Sub("j1")
ok, _ := j1.Exists(ctx, "status.json")
```

スキーム付きの絶対 URI を渡すと `ErrAbsoluteName` になります。スコープを絞ったつもりのコードが別のバケットへ書けてしまうのを防ぐためです。絶対 URI はルートのストアで扱ってください。

`Store` を包んで振る舞いを足す型（呼び出しを記録するテストのフェイクなど）は、`Sub` メソッドを `remoteio.Sub(d, prefix)` へ委譲してください。埋め込みから昇格した `Sub` は埋め込まれた側をスコープの土台にするため、包んだ側の振る舞いがスコープの先で失われます。

### 一覧を取る (`List`)

```go
for entry, err := range store.List(ctx, "gs://bucket/jobs", remoteio.WithDelimiter("/")) {
    if err != nil {
        return err
    }
    if entry.IsPrefix {
        // 疑似ディレクトリ。entry.Name は "j1/" のような相対名
        continue
    }
    fmt.Println(entry.URI, entry.Size)
}
```

`Entry.URI` は完全な URI なのでルートのストアへそのまま渡せます。`Entry.Name` は列挙したプレフィックスからの相対名なので、同じスコープ付きストアへそのまま渡せます。

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

`Close` と各アクセサは並行に呼ばれても安全です。`Close` は冪等で、以降のアクセサは `ErrClosed` を返します。

### 複数のクラウドを 1 つのストアで扱う

各ファクトリから `Handler` を取り出して並べるだけです。署名付き URL もスキームを見て振り分けられます。

```go
gcsHandler, err := gcsFactory.Handler()
s3Handler, err := s3Factory.Handler()

store := remoteio.NewStore(gcsHandler, s3Handler) // ローカルと file:// は自動で足されます

_ = store.Write(ctx, "gs://bucket/a.txt", src)
_ = store.Write(ctx, "s3://bucket/a.txt", src)
_ = store.Copy(ctx, "gs://bucket/a.txt", "s3://bucket/a.txt") // スキームを跨ぐコピー
```

各ファクトリの `Close` は呼び出し元が持ちます。

### 独自スキームを足す (`Handler`)

実装するのは `Handler` 1 本だけです。`Scheme()` が返す名前（区切りを含まない `"gs"` など）がそのまま振り分けのキーになり、空文字を返すハンドラはどのスキームにも一致しなかったパスを受け取るフォールバックになります。

```go
type Handler interface {
    Scheme() string

    Open(ctx context.Context, uri string) (io.ReadCloser, error)
    Stat(ctx context.Context, uri string) (ObjectInfo, error)
    List(ctx context.Context, uri string, opts ListOptions) iter.Seq2[Entry, error]
    Write(ctx context.Context, uri string, src io.Reader, opts WriteOptions) error
    Delete(ctx context.Context, uri string) error
}
```

`Copier`（サーバーサイドコピー）と `Signer`（署名付き URL）は任意インターフェースです。実装できるハンドラだけが実装します。

オプションの解決はライブラリ側で完結するため、実装側は解決済みの `ListOptions` / `WriteOptions` を受け取ります。URI の分解は `remoteio.ParseBucketURI`（キーは空でも可）または `remoteio.ParseObjectURI`（キー必須）を、組み立ては `remoteio.BuildURI` を使ってください。

書いたハンドラは適合性スイートに通してください。契約を満たしているかを 1 本のスイートが検査します。

```go
func TestConformance(t *testing.T) {
    storetest.TestHandler(t, func(_ *testing.T) storetest.Fixture {
        return storetest.Fixture{
            Handler:             myHandler,
            Root:                "my://bucket/conformance",
            SupportsContentType: true,
            SupportsMetadata:    true,
            SupportsIfNotExists: true,
            BucketScoped:        true,
        }
    })
}
```

### テストで使う (`memio`)

```go
h := memio.New(memio.WithScheme(remoteio.SchemeGCS)) // gs:// を名乗らせれば本番と同じ URI が書けます
store := remoteio.NewStore(h)

_ = h.Seed("gs://bucket/jobs/j1/status.json", []byte(`{"state":"queued"}`))

// 保存された設定を確かめる
opts, ok := h.Options("gs://bucket/jobs/j1/status.json")

// 障害を注入する
h = memio.New(memio.WithFailure(func(op, uri string) error {
    if op == "write" {
        return errors.New("storage down")
    }
    return nil
}))
```

`Seed` / `URIs` / `Len` / `Options` / `WithFailure` / `WithClock` があります。ストレージとしての振る舞い（一覧の畳み込み、不在の返し方、削除の単位）は本物と同じ適合性スイートを通っているため、フェイクと本物がずれることはありません。

---

## 🧩 主要インターフェース (Key Interfaces)

| インターフェース | 内容 |
| :--- | :--- |
| **`Reader`** | `Open` だけ。読むだけの関数はこれを受け取る |
| **`Writer`** | `Write` だけ。書くだけの関数はこれを受け取る |
| **`Store`** | `Reader` + `Writer` + `Stat` / `Exists` / `List` / `Delete` / `Copy` / `SignURL` / `Sub`。利用側の窓口 |
| **`Handler`** | 1 スキーム分の実装の契約。ライブラリを広げるときに実装するのはこれだけ |
| **`Copier`** / **`Signer`** | ハンドラの任意機能（サーバーサイドコピー / 署名付き URL） |
| **`Factory`** | クライアントの寿命を持ち、`Store()` と `Handler()` を返す。`gcs.ClientFactory` / `s3.ClientFactory` が実装 |

### スキームの語彙

公開するのは**区切りを含まない名前**だけです（RFC 3986 に合わせています）。

| 定数 | 値 |
| :-- | :-- |
| `SchemeGCS` / `SchemeS3` / `SchemeFile` | `"gs"` / `"s3"` / `"file"` |

区切りを足す操作はライブラリ内に閉じています。呼び出し側が `"://"` を自前で連結する必要はありません。

```go
switch remoteio.Scheme(uri) {
case remoteio.SchemeGCS, remoteio.SchemeS3:
    // クラウドストレージ
}

// 前方一致の判定。strings.HasPrefix だと "gsfoo://..." まで拾います
if remoteio.HasScheme(uri, remoteio.SchemeGCS) { ... }
```

### URI とパスのユーティリティ

`Scheme` / `HasScheme` / `IsRemote` / `ParseURI` / `ParseBucketURI` / `ParseObjectURI` / `BuildURI` / `ListPrefix` / `NormalizeBucketName`、およびパス操作の `Dir`（親ディレクトリ）/ `Join`（結合）/ `IndexedPath`（拡張子の前に連番を挿入）があります。

リモート URI は URL ではなく「スキーム + バケット + 生のキー」として扱うため、オブジェクト名に含まれる空白や `?` はエンコードされません。`net/url` を通すと `%20` やクエリ区切りに化け、エラーにならないまま別のキーを指します。

設定から読んだバケット**名**は `NormalizeBucketName` を通してから使ってください。コンソールから貼った `gs://my-bucket/` を素通しすると `gs://gs://my-bucket//path` という URI ができ、失敗するのは書き込みの時点になります。

---

## 📏 約束事 (Contracts)

### エラー

スキームを問わず同じ意味で返すことが、この抽象が成立するための条件です。判定はメッセージ文字列ではなく `errors.Is` で行えます。

| 番兵 | 意味 |
| :-- | :-- |
| `ErrNotExist`（= `fs.ErrNotExist`） | 対象が無い。`os.ErrNotExist` とも一致 |
| `ErrExist`（= `fs.ErrExist`） | `WithIfNotExists` で対象が既に在った |
| `ErrUnsupportedScheme` | どのハンドラも担当していないスキーム |
| `ErrClosed` | クローズ済みのファクトリを操作した |
| `ErrNotSupported` | ハンドラがその任意機能に対応していない |
| `ErrAbsoluteName` | スコープ付きストアへ絶対 URI を渡した |
| `ErrInvalidURI` | URI の形が想定と違う |

| 状況 | 返り値 |
| :--- | :--- |
| `Open` / `Stat` で対象が無い | `ErrNotExist` を包んだエラー |
| `Exists` で対象が無い | `(false, nil)`。エラーにはしません |
| `Delete` で対象が無い | `nil`。削除は冪等です |
| オブジェクト名が空の URI（`gs://bucket`） | `ErrInvalidURI`。バケット操作との取り違えを防ぎ、不在と URI 不正を区別するためです |
| ハンドラへ担当外のスキームを直接渡した | `ErrInvalidURI`。`Router` 経由なら振り分けの時点で弾かれます |

### 書き込み

**成功しなければ書き込み先は変化しません。** 途中で失敗した結果として切り詰められたオブジェクトが残ることはなく、既存オブジェクトの上書き中に失敗しても元の内容は保たれます。

`WithIfNotExists` を指定すると、対象が存在しない場合にのみ書き込みます。`Exists` で確かめてから `Write` する形はその 2 呼び出しの間に他のプロセスが割り込めますが、これは判定をストレージ側（GCS の前提条件 / S3 の `If-None-Match`）へ寄せます。

### 存在確認 (`Exists`)

「オブジェクトが在るか」だけを見ます。ローカルのディレクトリもリモートの疑似ディレクトリも対象になりません。リモートにディレクトリという実体は無いため、ローカルだけ真を返すと同じ呼び出しがスキームによって別の意味になります。階層の有無は `List` で見てください。

### 一覧取得 (`List`)

**プレフィックスは常に「その階層の中身」を指す形へ正規化されます。** `data` と `data/` は同じ結果になり、`data-archive/` は一致しません。正規化しないと素の文字列前方一致になり、スキームによって意味が変わります。

区切り文字を渡すかどうかで対象範囲が変わります。

| | 対象 | 疑似ディレクトリ |
| :--- | :--- | :--- |
| `WithDelimiter("/")` あり | 直下のみ | `IsPrefix` が真の `Entry` として併せて列挙 |
| なし（既定） | 配下を再帰的に、オブジェクトだけ | 列挙しない |

1 つの疑似ディレクトリに複数のオブジェクトがあるレイアウトでは、区切り文字なしの一覧はその数だけ返るため、呼び出し側で重複を潰すことになります。`WithDelimiter` はその走査をサーバー側へ寄せます。

**何も無いプレフィックスの一覧は、空でエラーになりません。** リモートに「存在しないプレフィックス」という状態が無いためで、ローカルだけエラーにすると同じ呼び出しがスキームによって別の意味になります。

### そのほか

* ローカルへの書き込みでは `Content-Type` やユーザー定義メタデータは保存されず、黙って無視されます。`Stat` の `ContentType` は空、`Metadata` は nil になります。
* ローカルへの書き込みは `context` のキャンセルを検知して打ち切ります（`os.File` への `io.Copy` は本来 ctx を見ません）。既存ファイルを差し替える場合はそのパーミッションを引き継ぎ、新規なら `0644` です。
* `file://` はローカルパスへ読み替えて扱います。`file:///tmp/a.txt` は `/tmp/a.txt` です。URI の規約どおりパーセントデコードするため、名前に `%` を含むファイルは `%25` と書く必要があります。一覧結果も `file://` 付きで返ります。
* S3 はユーザー定義メタデータのキーを小文字へ正規化するため、`Stat` で読み戻したときに大文字小文字が変わることがあります。
* 署名付き URL はスキーム厳格です。S3 は GET / PUT のみ対応で、それ以外は `ErrNotSupported` を返します。
* S3 のリージョンが未設定の場合は `ap-northeast-1`（`s3.DefaultRegion`）を既定値として使います。優先順は `WithRegion` > 環境変数や設定ファイル > 既定値です。

---

## 🛠️ 主要な依存関係 (Dependencies)

| サービス | パッケージ / リンク | 説明 |
| :--- | :--- | :--- |
| **GCS** | [cloud.google.com/go/storage](https://github.com/googleapis/google-cloud-go/tree/main/storage) | Google Cloud Storage 公式 Go クライアント（`remoteio/gcs` のみ） |
| **AWS S3** | [aws-sdk-go-v2](https://github.com/aws/aws-sdk-go-v2) | AWS SDK for Go v2。書き込みは [feature/s3/transfermanager](https://github.com/aws/aws-sdk-go-v2/tree/main/feature/s3/transfermanager) を通します（`remoteio/s3` のみ） |

テスト専用の依存として [testify](https://github.com/stretchr/testify)、および実 I/O を伴う統合テスト用に [fake-gcs-server](https://github.com/fsouza/fake-gcs-server) と [gofakes3](https://github.com/johannesboyne/gofakes3) を使用しています。どちらもプロセス内で起動するため、テストの実行に docker や認証情報は要りません。

---

## 📜 ライセンス (License)

このプロジェクトは [MIT License](https://opensource.org/licenses/MIT) の下で公開されています。
