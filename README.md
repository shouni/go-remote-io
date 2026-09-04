# 📁 Go Remote IO

[![CI](https://github.com/shouni/go-remote-io/actions/workflows/ci.yml/badge.svg)](https://github.com/shouni/go-remote-io/actions/workflows/ci.yml)
[![Status](https://img.shields.io/badge/Status-Active-brightgreen)](#)
[![Language](https://img.shields.io/badge/Language-Go-blue)](https://go.dev/)
[![Go Version](https://img.shields.io/github/go-mod/go-version/shouni/go-remote-io)](https://go.dev/)
[![GitHub tag (latest by date)](https://img.shields.io/github/v/tag/shouni/go-remote-io)](https://github.com/shouni/go-remote-io/tags)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Reference](https://pkg.go.dev/badge/github.com/shouni/go-remote-io.svg)](https://pkg.go.dev/github.com/shouni/go-remote-io)

## 🚀 概要 (About) - GCS・S3・ローカルを 1 つの Store で読み書きする。何を置くかは呼び出し側

Go Remote IO は、**Google Cloud Storage**・**Amazon S3**・**ローカルファイルシステム**を単一の
インターフェースで扱う I/O ライブラリです。`gs://` / `s3://` / `file://` / ローカルパスのいずれでも
`Open(ctx, name)` 一本で読め、書き込み・一覧・存在確認・削除・コピー・署名付き URL も同じ窓口から
呼べます。

引き受けるのは**保存先の違いを吸収すること**だけです。オブジェクトの命名規則も、何を成果物として
置くかも、資格情報の用意も呼び出し側に残ります。取り出したバイト列の解釈も持ちません。

### go-web-reader との線引き

姉妹ライブラリの [go-web-reader](https://github.com/shouni/go-web-reader) も `gs://` を読みますが、
担当している工程が違います。**go-remote-io は成果物の置き場**で、読み書き両方に加えて署名付き URL と
一覧を持ち、ローカルも扱います。**go-web-reader は素材の取得元**で、`https://` を含む読み取り専用、
HTML は本文だけを抽出します。項目ごとの対照表は
[go-web-reader の README](https://github.com/shouni/go-web-reader#go-remote-io-との線引き) にあります
（両方に置くと必ず片方が古くなるため、後発の側に 1 つだけ置いています）。

---

## ✨ 提供機能 (Features)

* **スキームに応じた振り分け** — `gs://` / `s3://` / `file://` / それ以外はローカル、を見て登録済みの
  ハンドラへ委譲します。**対応スキームは構築時に決まり**、登録されていないスキームは
  `ErrUnsupportedScheme` で明確に拒否されます。設定漏れと非対応を取り違えません。
* **1 つの窓口 (`Store`)** — 読み書き・一覧・存在確認・メタデータ・削除・コピー・署名付き URL を
  1 つのインターフェースが持ちます。依存を絞りたい関数は `Reader`（`Open` だけ）や
  `Writer`（`Write` だけ）を受け取れます。
* **スコープ付きストア (`Sub`)** — プレフィックスに固定したストアを作れます。呼び出しのたびに
  バケット名を連れ回す必要がなくなります。
* **原子的な書き込み** — 成功しなければ書き込み先は変化しません。ローカルは一時ファイル + `rename`、
  GCS はアップロードの中断、S3 はマルチパートの abort で実現しています。条件付き書き込み
  (`WithIfNotExists`) も使えます。
* **型のついた一覧** — `List` は `iter.Seq2[Entry, error]` を返し、疑似ディレクトリは `Entry.IsPrefix`
  で分かります。`break` でそのまま打ち切れます。
* **サーバーサイドコピー** — `Store.Copy` は、同じスキーム内でハンドラが対応していればサーバー側の
  コピーへ落とし、そうでなければストリームで中継します。呼び出し側に分岐は要りません。
* **スキームに依らないエラー** — 「見つからない」は必ず `ErrNotExist`（`io/fs` と同じ値）を包んで
  返るため、保存先を問わず `errors.Is` 一本で判定できます。
* **クラウド SDK 非依存のコア** — `remoteio` パッケージ自体は GCS / AWS の SDK を import しません。
* **テスト用のインメモリ実装** — `memio` が本物と同じ契約を満たすハンドラを提供します。手書きの
  フェイクは要りません。
* **接続先の差し替え** — MinIO や Cloudflare R2 のような S3 互換ストレージ、生成済みクライアントの
  再利用に対応します。
* **`io/fs` との相互運用** — `remoteio.FS(ctx, store)` で読み取り専用の `fs.FS` として渡せます。

インターフェースの定義、番兵エラー、書き込み・一覧のオプション、`gcs` / `s3` の接続オプションは
[pkg.go.dev](https://pkg.go.dev/github.com/shouni/go-remote-io/remoteio) にあります。

---

## 📦 パッケージ構成 (Package Structure)

```text
go-remote-io/
└── remoteio/       # 抽象（Store / Handler / Entry / エラーの語彙 / URI とパスの操作）とローカル・file:// の実装
    ├── gcs/        # GCS 具象実装。クライアントの寿命と Handler / Copier / Signer
    ├── s3/         # S3 具象実装。同上
    ├── memio/      # インメモリの Handler。テストで本物の代わりに使う
    └── storetest/  # Handler の適合性テストスイート
```

**クラウド SDK に触れてよいのは `gcs` と `s3` だけです。** 抽象だけを使うアプリケーションのビルドに
SDK を持ち込まないための境界なので、`remoteio` 直下に SDK 依存を足さないでください。

新しいストレージへ広げるときに実装するのは `Handler` 1 本だけです。書いたハンドラは
`storetest.TestHandler` に通してください。GCS / S3 / ローカル / `memio` / 第三者の実装が、すべて同じ
1 本のスイートで検査されます。これが無いと、フェイクと本物がずれても CI は緑のままになります。

---

## 🚦 使い方 (Usage)

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

`Sub` が返すストアはそのプレフィックスからの**相対名だけ**を受け取ります（絶対 URI を渡すと
`ErrAbsoluteName` です）。複数のクラウドを 1 つのストアにしたい場合は、各ファクトリの `Handler()` を
`remoteio.NewStore` へ並べてください。ローカルと `file://` は自動で足されます。

**踏むと高くつく点も、それぞれの godoc に書いてあります** — 何も無いプレフィックスの一覧が空で
返りエラーにならないこと、区切り文字を渡さない一覧が配下を再帰的に返すこと、リモート URI は URL
ではないので `net/url` ではなく `Dir` / `Join` / `BuildURI` を使うこと。

---

## 🤝 依存関係 (Dependencies)

* [cloud.google.com/go/storage](https://github.com/googleapis/google-cloud-go/tree/main/storage) -
  GCS 公式 Go クライアント（`remoteio/gcs` のみ）
* [aws-sdk-go-v2](https://github.com/aws/aws-sdk-go-v2) - AWS SDK for Go v2。書き込みは
  [feature/s3/transfermanager](https://github.com/aws/aws-sdk-go-v2/tree/main/feature/s3/transfermanager)
  を通します（`remoteio/s3` のみ）

テスト専用の依存として [testify](https://github.com/stretchr/testify)、実 I/O を伴う統合テスト用に
[fake-gcs-server](https://github.com/fsouza/fake-gcs-server) と
[gofakes3](https://github.com/johannesboyne/gofakes3) を使っています。どちらもプロセス内で起動するため、
テストの実行に docker や認証情報は要りません。

---

## 📜 ライセンス (License)

このプロジェクトは [MIT License](https://opensource.org/licenses/MIT) の下で公開されています。
