# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## このリポジトリについて

`github.com/shouni/go-remote-io` は GCS / S3 / ローカルファイルシステムを単一のインターフェースで扱うライブラリです。

コード内のコメント・エラーメッセージ・README はすべて日本語です。新規コードも日本語に揃えます。ただし**番兵エラーの文言だけは英語**で、`remoteio: ` を接頭辞に付けます（番兵は識別子、ラップは読み手向けの文脈、という切り分け）。

### 破壊的変更の扱い

現在、**v1.10.1 からの全面的な作り直し（旧 v2 計画）を v1 系のタグで出す途中**です。利用者が 1 人で、過去の GCS 成果物が壊れても構わないという前提のもと、`/v2` のモジュールパスを使わないフラグデー方式を選んでいます。依存 9 リポジトリの CI は同時に赤くなります。

* モジュールパスは `github.com/shouni/go-remote-io` のまま、パッケージも `remoteio/` のままです。**利用側の import 行は 1 行も変わりません**（変わるのは API の使い方だけ）。
* この作り直しが全リポジトリへ行き渡るまでは、エクスポートされた識別子の変更・削除に遠慮は要りません。行き渡ったあとは通常の互換性配慮へ戻します。

## コマンド

```bash
go build ./...
go vet ./...
test -z "$(gofmt -l .)"        # CI と同じフォーマットチェック
go test -race ./...            # CI はレースディテクタ有効で実行
golangci-lint run              # v2 設定 (.golangci.yml)、CI は v2.12.2
govulncheck ./...
```

`golangci-lint` / `govulncheck` はローカルにインストール済み (`~/go/bin`)。

## アーキテクチャ

### 入口は Store、拡張点は Handler の 1 本だけ

```go
factory, err := gcs.New(ctx)        // クライアントのライフサイクルを持つ
defer factory.Close()
store, err := factory.Store()       // gs:// + ローカル + file:// を扱う

jobs := store.Sub("gs://bucket/jobs")             // 組み立て時に一度だけスコープを決める
err = jobs.Write(ctx, id+"/status.json", body, remoteio.WithContentType("application/json"))
```

* **`Store`** (`store.go`) が利用側の窓口です。`Open` / `Stat` / `Exists` / `List` / `Write` / `Delete` / `Copy` / `SignURL` / `Sub` を持ちます。依存を絞りたい関数は `Reader` (`Open` だけ) や `Writer` (`Write` だけ) を受け取ってください。
* **`Handler`** (`handler.go`) が唯一の拡張点です。新しいストレージへ広げるときに実装するのはこれ 1 本。オプションの解決はライブラリ側で完結するため、実装側は解決済みの `ListOptions` / `WriteOptions` を受け取ります。
  * 以前は拡張点が 2 系統ありました（可変長オプションを受ける `Lister` などと、解決済み設定を受ける `SchemeHandler`）。そのために `NewListSettings` / `ListPrefix` / `NewWriteSettings` を公開する必要があり、しかもそれらを使っていたのはテストのフェイクだけでした。
  * `Exists` は `Handler` にありません。`Stat` + `errors.Is(err, ErrNotExist)` から導出できるので、実装が 1 つ減り「不在を `(false, nil)` で返す」約束を取り違える余地も消えます。
* **`Copier` / `Signer`** (`handler.go`) は任意インターフェースです。実装できるハンドラだけが実装します。`Store.Copy` は src と dst が同じスキームに解決され、かつ `Copier` を実装しているときだけサーバーサイドコピーへ落とし、そうでなければストリームで中継します。**呼び出し側に分岐は要りません。**
  * `CopyTo` が `ErrNotSupported` を返した場合もストリーム中継へ落ちます。構築時に対応可否を決められないハンドラ (`Lazy`) が実行時にそれを伝える経路です。
* **`Router`** (`router.go`) が `Store` の実体で、URI のスキームを見てハンドラを引きます。振り分けが実行時なのは、パスが設定やユーザー入力から来る以上どのスキームを扱うか静的に決められないためです。一方「どのスキームに対応しているか」は構築時に決まるので、ハンドラの集合という明示的なデータとして持ちます。**未対応の判定は `Router.resolve` の 1 箇所だけ**です。
* `Scheme()` が空文字のハンドラ (`NewLocalHandler`) はフォールバックになり、スキームを持たないパスを受け取ります。`NewStore` はリモート用ハンドラにローカルと `file://` を必ず組にして登録するので、**gcs ファクトリから得た Store でもローカルパスは読めます**が、`s3://` は未登録として明確に拒否されます。
* 複数クラウドを 1 つの `Store` にするには、各ファクトリの `Handler()` を `NewStore` へ並べるだけです。以前はこれに `MultiFactory` と `HandlerProvider`、そして署名器専用の `signerRouter` が必要でした。`Signer` を任意インターフェースにしたので振り分けは `Router` の 1 箇所へ戻っています。
* `Router.Schemes()` は辞書順で返します。map の反復順のままだと、公開 API なのに呼び出しごとに結果が変わります。

### `Sub` — スコープ付きストア

`Sub` が返す `Store` はプレフィックスに固定され、そこからの**相対名だけ**を受け取ります。スキーム付きの絶対 URI を渡すと `ErrAbsoluteName` です（スコープを絞ったつもりのコードが別のバケットへ書けてしまうのを防ぐため）。絶対 URI を扱いたい場合はルートの `Store` を使います。

これが無かった頃は、利用側が `BuildGCSURI(bucket, path)` を呼ぶ薄いメソッドをリポジトリごとに書き直しており、それでも足りずに `"gs://"` を直書きする箇所がエコシステム全体で 24 箇所ありました。

`Entry.Name` は列挙したプレフィックスからの相対名なので、そのまま同じスコープ付きストアへ渡せます。`Entry.URI` は完全な URI なのでルートの `Store` へ渡せます。

### 書き込みの原子性は契約

`Handler.Write` は「**成功しなければ書き込み先が変化しない**」ことを保証しなければなりません。途中で失敗した結果として切り詰められたオブジェクトが残ってはいけません。3 実装それぞれの満たし方:

* **ローカル** — 同じディレクトリの一時ファイルへ書き、`Sync` してから `Rename` します。`WithIfNotExists` のときは `Rename` ではなく `Link` を使います（対象が在れば EEXIST で失敗するため、確認と作成の間に割り込まれません）。`CreateTemp` は 0600 で作るので、差し替え前に既存ファイルの権限を引き継ぐか 0644 に戻します。権限はパスではなく開いているディスクリプタへ適用します。
* **GCS** — `io.Copy` が失敗したら、**パイプにエラーを立ててから `Close`** します。順序が要点です:
  * `storage.Writer.Close()` はアップロードを「**完了**」させる API です。失敗経路でそのまま呼ぶと中途半端なオブジェクトが残ります（これが旧実装の実バグでした）。
  * `cancel()` だけでは足りません。アップロード用の goroutine が走ったまま `Write` が返り、誰も終了を待ちません。加えて **GCS フェイクはキャンセル済み ctx でもアップロードを完了させる**ため、ctx 依存の中断はトランスポート任せになります。
  * よって `CloseWithError` でパイプにエラーを立て（リクエストが必ず失敗する）、続けて `Close` を呼んで `<-donec` で goroutine の後始末まで待ちます。
  * `io.Copy` が 1 バイトも書けなかった場合は Writer が未オープンです。ここで `Close` すると `openWriter` が走ってゼロバイトのオブジェクトができるため、**触りません**。
* **S3** — `feature/s3/transfermanager` の `UploadObject` を通します。`PutObject` 直呼びはボディが `io.Seeker` でないと署名前のチェックサム計算に失敗し、TLS でないエンドポイント（MinIO / R2 / テストのフェイク）では書き込みが一切できませんでした。旧 `feature/s3/manager` の `Uploader` は非推奨（discussion #3306）なので後継を使います。後継は v0.x で API が動く可能性がありますが、このエコシステムで `s3://` を使っているのは 1 箇所だけなので追随コストより非推奨 API を抱えない方を採っています。

### `List` は反復子、`Entry` は型付き

```go
for entry, err := range store.List(ctx, "jobs", remoteio.WithDelimiter("/")) {
    if err != nil { return err }
    if entry.IsPrefix { ... }
}
```

* 疑似ディレクトリは `Entry.IsPrefix` で分かります。以前は callback に文字列だけを渡していたため、呼び出し側が末尾の区切り文字を見て判定していました。GCS は `attrs.Prefix`、S3 は `CommonPrefixes` という**型のついた情報を持っていたのに文字列へ潰していた**のが原因です。
* 反復の途中で失敗したら、ゼロ値の `Entry` と共にエラーを 1 度だけ yield して打ち切ります。`break` はそのまま反復を止めます。
* **プレフィックスは常に `ListPrefix` で正規化されます** (`data` → `data/`)。以前は区切り文字を指定したときだけ正規化しており、指定しなければ GCS / S3 では素の前方一致（`data` が `data-archive/` にも一致）、ローカルはディレクトリ走査、というスキーム依存の食い違いが残っていました。
* 区切り文字なしのローカル一覧は `filepath.WalkDir` で再帰的にファイルだけを返します。GCS / S3 がプレフィックス配下を再帰的に返すのに対しローカルだけ直下で止まると、同じ呼び出しがスキームによって別の意味になります。

### エラーの約束事

番兵は `errors.go` に集約しています。呼び出し側がメッセージ文字列ではなく `errors.Is` で分岐できることが、この抽象が成立する条件です。

| 番兵 | 意味 |
| :-- | :-- |
| `ErrNotExist` (= `fs.ErrNotExist`) | 対象が無い。`os.ErrNotExist` とも一致 |
| `ErrExist` (= `fs.ErrExist`) | `WithIfNotExists` で対象が既に在った |
| `ErrUnsupportedScheme` | どのハンドラも担当していないスキーム |
| `ErrClosed` | クローズ済みのファクトリを操作した |
| `ErrNotSupported` | ハンドラがその任意機能に対応していない |
| `ErrAbsoluteName` | スコープ付きストアへ絶対 URI を渡した |
| `ErrInvalidURI` | URI の形が想定と違う |

* ラップは `wrapf` (`errors.go`) を通します。番兵を保ったまま日本語の文脈を付ける形を全ファイルで揃えるためです。
* **`Exists` はオブジェクトの有無だけを見ます。** ローカルのディレクトリもリモートの疑似ディレクトリも `false` です。ローカルの `Stat` と `Open` はディレクトリに `ErrNotExist` を返します（`os.Open` はディレクトリでも成功し読む時点で初めて失敗するため、そのままだとローカルだけ「開けるが読めないもの」が存在します）。階層の有無は `List` で見てください。
* オブジェクト単位の操作は、オブジェクト名が空の URI (`gs://bucket` など) を明示的に拒否します (`ParseObjectURI`)。バケット操作と取り違えたり、不在なのか URI 不正なのか区別できなくなるのを防ぐためです。一覧だけはプレフィックスが空でも意味を持つので `ParseBucketURI` を使います。
* ハンドラは担当外のスキームを拒否します。`Store` 経由なら振り分けで弾かれますが、ハンドラは公開されているため直接呼ばれる余地があります（以前は `gcs.Handler.Open(ctx, "s3://b/k")` が GCS のバケット b を読んでいました）。
* S3 の型付きエラー判定は `errors.AsType[*types.NoSuchKey](err)` を使います (Go 1.26 の `errors.AsType`)。`HeadObject` は `NotFound` も返すため両方見ます。条件付き書き込みの失敗は `smithy.APIError` の `PreconditionFailed` で判定します。

### スキームの語彙

定数は `uri.go` の 1 ブロックに集約しています。**公開するのは区切りを含まない名前 (`SchemeGCS = "gs"`) だけ**で、`Handler.Scheme()` も `Router` の登録キーもこの形です（RFC 3986 に合わせています。以前はプレフィックス `"gs://"` の形を公開していました）。

区切りを足す操作は `uri.go` の中だけに閉じています。呼び出し側が `"://"` を自前で連結する必要はありません:

* `Scheme(uri)` — URI からスキーム名を取り出す（ローカルパスなら空文字）
* `HasScheme(uri, scheme)` — 判定。素の `strings.HasPrefix` だと `"gsfoo://..."` まで拾います
* `BuildURI(scheme, bucket, object)` / `ParseURI(uri)`

### URI とパスのユーティリティ

`uri.go` のパス操作 (`Dir` / `Join`) は `net/url` を通しません。`gs://` / `s3://` は URL ではなく「スキーム + バケット + 生のキー」で、オブジェクト名の空白や `?` を URL として組み直すと `%20` やクエリ区切りに化けます。`ParseURI` はデコードしないため、化けた URI はエラーにならずに別のキーを指します。同じ理由で `filepath.Dir` もリモート URI には使いません（Windows でセパレータが混ざります）。

### `Lazy` と `FS`

* **`Lazy(scheme, open)`** (`lazy.go`) は初回の操作までハンドラの生成を遅らせます。対応スキームを構築時に宣言したいけれど、そのスキームが使われるとは限らない場面（HTTP も GCS も S3 も受け付けるリーダーなど）で、使いもしないクラウドの認証で起動が失敗するのを避けます。生成は成功・失敗とも 1 度だけです。
* **`FS(ctx, store)`** (`fs.go`) は `Store` を読み取り専用の `io/fs.FS` として見せます。`io/fs` をコア抽象にしていないのは `fs.FS.Open` が context を取らないためです。ネットワーク I/O にキャンセルと期限を渡せないインターフェースは土台にできません。アダプタ側では ctx を構造体に持たせています（通常は避ける形ですが、`fs.FS` を満たす唯一の方法です）。

### ファイル分割の規則

`remoteio` パッケージは**クラウド SDK を import しません**。GCS/AWS の SDK に触れてよいのは `remoteio/gcs` と `remoteio/s3` だけです。抽象だけを使うアプリケーションのビルドにクラウド SDK を持ち込まないための境界なので、`remoteio` 直下に SDK 依存を足さないでください。

各スキームパッケージは `factory.go`（クライアントのライフサイクルと `Option`）、`handler.go`（`Handler` と `Copier` の実装）、`signer.go`（`Signer` の実装）の 3 つに分かれます。URI の分解は `parseObjectURI`（オブジェクト名必須）か `parseBucketURI`（一覧など、プレフィックスが空でもよい場合）を通してください。どちらもクライアントの nil チェックを含み、その先は `remoteio` 側の共通関数です。

### ファクトリ

* `Factory` (`store.go`) は `Close` / `Store()` / `Handler()` を要求します。`Handler()` があるので、複数クラウドを束ねるための別インターフェースは要りません。
* 生成方法は `Option` で差し替えます (`gcs.WithClient` / `s3.WithEndpoint` など)。`WithClient` で注入されたクライアントのライフサイクルは呼び出し元に残り、`Close` は閉じません（GCS は `ownsClient` で管理）。
* **`Close` と各アクセサは並行に呼ばれても安全です** (`sync.RWMutex`)。以前はクライアントのフィールドを無同期で nil にしていました。CI は `-race` 付きですが、並行に触るテストが無く検出されていませんでした。`Close` は冪等で、以降のアクセサは `ErrClosed` を返します。
* S3 のリージョン未設定時のデフォルトは `ap-northeast-1` (`s3.DefaultRegion`)。優先順は `WithRegion` > 環境や設定ファイル > 既定値。
* 署名付き URL はスキーム厳格です。S3 は GET / PUT のみ対応し、それ以外は `ErrNotSupported` を返します。

## テスト

* testify (`require` で前提、`assert` で検証) + `t.Run` サブテスト。テーブル駆動が基本。
* リモートパスの読み書き・一覧・存在確認は**インプロセスのフェイク**で実際に動かします (`remoteio/gcs/handler_integration_test.go` が `fsouza/fake-gcs-server`、`remoteio/s3/handler_integration_test.go` が `johannesboyne/gofakes3`)。どちらも Go ライブラリとしてプロセス内で起動するため docker も認証情報も不要です。
* **フェイクは本物と違うことがあります。** GCS フェイクはキャンセル済み ctx でもアップロードを完了させ、gofakes3 は平文 HTTP なので非 Seeker ボディの条件が本番と変わります。どちらも実装の弱点を暴いてくれた差分なので、フェイク前提の回避策ではなく実装側を直してください。フェイクが解釈しない機能を検証する場合は、黙って通さず `t.Skip` に理由を残します。
* `remoteio/multi_integration_test.go` は `package remoteio_test`（外部テストパッケージ）で、GCS と S3 の両フェイクを 1 つの `Store` に束ねて検証します。**クロスクラウドのコピーはここでしか通りません**（コピー元のストリームが `io.Seeker` でないため、S3 側の書き込み実装の回帰がここで出ます）。外部テストパッケージなので、`remoteio` 本体の依存にクラウド SDK は入りません。
* `io/fs` アダプタは `fstest.TestFS` に通します。名前の検証・`ReadDir` の並び・`Stat` と `Open` の整合といった規約を自前のアサーションで書き直さずに済み、実際にディレクトリ扱いの不整合を 3 件検出しました。
* `Close` と各アクセサを並行に呼ぶテスト (`TestFactoryCloseIsRaceFree`) を各ファクトリに置きます。無いと `-race` があっても競合を検出できません。
* インターフェース充足はパッケージ変数のコンパイル時チェックで担保します (`var _ remoteio.Copier = (*gcs.Handler)(nil)` など)。`Copier` の実装が外れると `Copy` は黙ってストリーム中継へ落ち、遅くなるだけで誰も気づきません。

## コミット / ブランチ

Conventional Commits (`feat(remoteio):`, `fix:`, `chore(deps):`, `test:`, `refactor:`, `docs:`)。CI は `main` と `develop` への push / PR で走ります。作業ブランチは `develop`、PR 先は通常 `main`。
