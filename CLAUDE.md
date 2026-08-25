# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## このリポジトリについて

`github.com/shouni/go-remote-io` は GCS / S3 / ローカルファイルシステムを単一のインターフェースで扱うライブラリです。実行バイナリ (`main` パッケージ) は無く、公開 API がそのまま成果物です。タグ付きリリース (v1.7.0 系。develop に v1.8.0 相当の変更あり) を打っているため、**エクスポートされた識別子の変更・削除は破壊的変更**として扱ってください。

コード内のコメント・エラーメッセージ・README はすべて日本語です。新規コードも日本語に揃えます。

## コマンド

```bash
go build ./...
go vet ./...
test -z "$(gofmt -l .)"        # CI と同じフォーマットチェック
go test -race ./...            # CI はレースディテクタ有効で実行
golangci-lint run              # v2 設定 (.golangci.yml)、CI は v2.12.2
govulncheck ./...
```

単体で走らせる場合（サブテストは `/` で指定）:

```bash
go test -run 'TestListWithDelimiterLocal' ./remoteio/
go test -run 'TestRouterLocal/List: stops and returns callback error' ./remoteio/
```

`golangci-lint` / `govulncheck` はローカルにインストール済み (`~/go/bin`)。

## アーキテクチャ

### スキーム振り分けは「呼び出し時」、対応スキームは「構築時」

パスは設定やユーザー入力から来るため、どのストレージを触るかは静的に決められません。一方「どのスキームに対応しているか」は構築時に決まる情報です。この 2 つを分けているのが `Router` (`remoteio/router.go`) と `SchemeHandler` (`remoteio/handler.go`) です。

* `Router` は `SchemeHandler` の集合を持ち、`InputReader` と `OutputWriter` の**両方**を満たします。`resolve` が `path` からスキームを取り出してハンドラを引き、未登録なら「未対応のURIスキームです」で返します。**未対応の判定はこの 1 箇所だけ**です。
* `Scheme()` が空文字のハンドラ (`NewLocalHandler`) はフォールバックになり、スキームを持たないパス（ローカル）を受け取ります。`gcs.ClientFactory` / `s3.ClientFactory` は `NewSchemeRouter` を通して自分のスキームとローカル（フォールバックと `file://`）を登録するので、**gcs ファクトリから得た Reader でもローカルパスは読めます**が、`s3://` は未登録として明確に拒否されます。
* 以前はひとつの具象型が GCS と S3 のクライアントを両方保持し、注入されていない方を nil にすることで「未対応」を表現していました。その形だと未対応であることが呼び出し時のエラー文字列でしか分からず、同じ nil ガードが操作ごとに散らばります。ハンドラの集合という明示的なデータに置き換えたのがこの構造です。
* 値を返す操作の振り分けはジェネリックな `dispatch[T]` (`remoteio/router.go`) を通します。以前は Reader 側だけ戻り値の型が揃わず if 連鎖のままでしたが、型パラメータで統一しました。値を返さない操作 (`List` / `Write` / `Delete`) は対になる `run` を通します。
* `Router.Schemes()` は辞書順で返します。map の反復順のままだと、公開 API なのに呼び出しごとに結果が変わります。
* `file://` は `handler_file.go` の `fileHandler` がローカルパスへ読み替えて `localHandler` へ委譲します。ローカルの実装を 2 つに増やさないための形です。`NewSchemeRouter` (`remoteio/factory.go`) がリモート用ハンドラにローカルと `file://` を必ず組にして登録し、両ファクトリはこれを使います。
* `MultiFactory` (`remoteio/multi.go`) は複数の `IOFactory` を 1 つに束ねます。`IOFactory` からは担当スキームが分からないため、束ねる側は `HandlerProvider`（`SchemeHandler() (SchemeHandler, error)`）を要求します。両 `ClientFactory` が実装しています。署名器は各々が自分のスキーム以外を拒否するので、`signerRouter` がスキームで振り分けます。

### ファイル分割の規則

`remoteio` パッケージは**クラウド SDK を import しません**。GCS/AWS の SDK に触れてよいのは `remoteio/gcs` と `remoteio/s3` だけです。抽象だけを使うアプリケーションのビルドにクラウド SDK を持ち込まないための境界なので、`remoteio` 直下に SDK 依存を足さないでください。

各スキームパッケージは `factory.go`（クライアントのライフサイクルと `Option`）、`handler.go`（`SchemeHandler` の実装）、`signer.go`（署名付き URL）の 3 つに分かれます。新しいストレージ操作を追加するときは、`SchemeHandler` にメソッドを足し、各パッケージの `handler.go` に実装します。URI の分解は `parseObjectURI`（オブジェクト名必須）か `parseBucketURI`（一覧など、プレフィックスが空でもよい場合）を通してください。どちらもクライアントの nil チェックを含み、その先は `remoteio` 側の共通関数です。

`Reader` / `Writer` を組み合わせただけの補助関数（`Copy` / `Move` / `ReadAll` / `Files` / `Stat`）は `remoteio/ops.go` に置きます。インターフェースを増やさずに済む操作をここに集めています。

ファクトリの生成方法は `Option` で差し替えます (`gcs.WithClient` / `s3.WithEndpoint` など)。`WithClient` で注入されたクライアントのライフサイクルは呼び出し元に残り、`Close` は閉じません（GCS は `ownsClient` で管理）。

### `Stat` を `InputReader` に入れていない理由

`Stat` は `SchemeHandler` には足していますが、`InputReader` には**含めていません**。`InputReader` は利用側が実装する複合インターフェースで、テストのフェイクや代替実装が世の中にあります。ここにメソッドを足すとそれらが一斉にコンパイルできなくなるため、`Stater` を独立させ、`*Router` が満たす形にしました。呼び出し側は `remoteio.Stat(ctx, reader, path)` を使えば型アサーションを書かずに済みます。

### 2 系統の Functional Options

| | 設定型 | 解決関数 |
| :-- | :-- | :-- |
| `WriteOption` (`write_options.go`) | `WriteSettings` | `NewWriteSettings` |
| `ListOption` (`list_options.go`) | `ListSettings` | `NewListSettings` |

どちらの設定型も公開しています。`Lister` / `SchemeHandler` を実装する第三者やテストのフェイクが、中身を読めないままオプションを無視してしまう状態を避けるためです (`ListSettings` の公開は commit ed05c68、`WriteSettings` は `SchemeHandler` が別パッケージから設定を読む必要が出たときに揃えました)。オプションを増やす場合は設定型にフィールドを足し、実装側が `New*Settings(opts...)` で同じ解決結果を得られる形を保ちます。

`WriteOption` の `Content-Type` などのメタデータはローカル書き込みでは黙って無視されます (`handler_local.go` のコメント参照)。

### `WithDelimiter` の意味論

区切り文字を渡すと prefix 直下だけが対象になり、疑似ディレクトリが**区切り文字で終わる URI** として callback に渡されます。呼び出し側は末尾で判別します。実装上の要点:

* prefix は `ListPrefix` で正規化される (`data` → `data/`)。正規化しないと `data-archive/` まで一致するため。
* GCS は疑似ディレクトリが `attrs.Name` 空 + `attrs.Prefix` に、S3 は `Contents` ではなく `CommonPrefixes` に入る。prefix 自身は除外する。
* ローカルの一覧は、区切り文字ありなら直下のみ（ディレクトリを区切り文字付きで併せて返す）、区切り文字なしなら `filepath.WalkDir` で再帰的にファイルだけを返します。GCS / S3 が prefix 配下を再帰的に返すのに対しローカルだけ直下で止まると、同じ呼び出しがスキームによって別の意味になり、呼び出し側からその違いが見えないためです。
* ただし**区切り文字なしの prefix は、GCS / S3 では素の文字列前方一致**です（`data` は `data-archive/` にも一致する）。`ListPrefix` の正規化は区切り文字を指定したときだけ働きます。ローカルはファイルシステムの性質上ディレクトリ単位の走査になるため、ここだけは意味が揃いません。

### エラーの約束事

* `Open` / `Stat` の「見つからない」は `os.ErrNotExist` を `%w` でラップして返す (GCS/S3 とも)。呼び出し側が `errors.Is` でスキーム非依存に判定できるようにするため。
* `Exists` は不在を `(false, nil)` で返し、それ以外の失敗のみエラーにする。
* オブジェクト単位の操作 (`Open` / `Stat` / `Exists` / `Write` / `Delete`) はオブジェクト名が空の URI (`gs://bucket` など) を明示的に拒否する (`ParseSchemeObjectURI`)。バケット操作と取り違えたり、不在なのか URI 不正なのか区別できなくなるのを防ぐため。一覧だけはプレフィックスが空でも意味を持つので `ParseSchemeURI` を使う。
* S3 の型付きエラー判定は `errors.AsType[*types.NoSuchKey](err)` を使用 (Go 1.26 の `errors.AsType`)。`HeadObject` は `NotFound` も返すため両方見る。
* ローカル書き込みは `io.Copy` に `ctxReader` を挟んで ctx キャンセルを検知します。`os.File` への `io.Copy` は ctx を見ないため、これを外すと巨大ファイルを最後まで書き切ってしまいます。
* ローカル書き込みは同じディレクトリの一時ファイルへ書いてから `os.Rename` します。`os.Create` へ直接書くと、キャンセルや I/O エラーで抜けたときに中途半端なファイルが残ります。リモート側は失敗すればオブジェクトができないので、ここだけ挙動が違うと呼び出し側が両方に備えることになります。`os.CreateTemp` は 0600 で作るため、`rename` の前に 0644 へ戻しています。
* ハンドラは担当外のスキームを拒否します (`ParseSchemeURI`)。`Router` 経由なら振り分けの時点で弾かれますが、ハンドラは公開されているため直接呼ばれる余地があります。以前は `gcs.Handler.Open(ctx, "s3://b/k")` が GCS のバケット b を読んでいました。

### URI とパスのユーティリティ

* URI の分解はスキーム非依存の `ParseBucketURI` / `BuildURI` (`remoteio/uri.go`) が土台で、`ParseRemoteURI` / `BuildGCSURI` / `BuildS3URI` はその上の薄いラッパーです。`SchemeHandler` と `Router` がスキーム非依存なのに分解だけ `gs://` / `s3://` 直書きだと、第三者が新しいスキームのハンドラを書いたときに「どこからがバケット名か」の解釈がずれます。
* `path.go` のパス操作 (`ResolveBaseDir` / `ResolvePath`) は `net/url` を通しません。`gs://` / `s3://` は URL ではなく「スキーム + バケット + 生のキー」で、オブジェクト名の空白や `?` を URL として組み直すと `%20` やクエリ区切りに化けます。`ParseRemoteURI` はデコードしないため、化けた URI はエラーにならずに別のキーを指します。同じ理由で `filepath.Dir` も URL パスには使いません（Windows でセパレータが混ざります）。

### ファクトリと署名付き URL

* `IOFactory` は `Close` / `InputReader` / `OutputWriter` / `URLSigner` を要求。両ファクトリはインターフェース外の `Reader()` / `Writer()` も持ち、複合インターフェース側へ委譲しています。
* `remoteio.Bundle` (`bundle.go`) は、その 3 アクセサを一度に取り出して保持する構造体です。利用側が全く同じ組み立て関数と構造体を各自持っていたものを引き取ったもので、**所有権の受け渡しが要点**です。`NewBundle` は成功した場合にのみ factory のライフサイクルを引き取り（以降 `Bundle.Close` が閉じる）、失敗時は閉じずに返します。組み立て途中の後始末を、他の資源とまとめて呼び出し元が行えるようにするためです。`Close` が nil レシーバーを許容するのは `[]io.Closer` へ入れて一括解放される使われ方に備えたもの。
* `Bundle` を `Client` と呼ばないのは粒度が違うためです。この型は接続を持たず I/O もせず、他が実装したインターフェースを束ねているだけで、`Close` 以外のメソッドを持ちません。
* `Close` 後はクライアントを nil にし、以降のアクセサはエラーを返す (S3 の `Close` は SDK 的には不要だが同じ契約に揃えている)。`Close` は冪等。
* 署名付き URL はスキーム厳格: `gcs.NewURLSigner` は `gs://` 以外、`s3.NewURLSigner` は `s3://` 以外を拒否。S3 は GET / PUT のみ対応。
* S3 のリージョン未設定時のデフォルトは `ap-northeast-1` (`s3.DefaultRegion`)。優先順は `WithRegion` > 環境や設定ファイル > 既定値。

## テスト

* testify (`require` で前提、`assert` で検証) + `t.Run` サブテスト。テーブル駆動が基本。
* リモートパスの読み書き・一覧・存在確認は**インプロセスのフェイク**で実際に動かします (`remoteio/gcs/handler_integration_test.go` が `fsouza/fake-gcs-server`、`remoteio/s3/handler_integration_test.go` が `johannesboyne/gofakes3`)。どちらも Go ライブラリとしてプロセス内で起動するため docker も認証情報も不要で、通常の `go test ./...` で走ります。以前はここが未検証で、疑似ディレクトリの扱い（GCS は `attrs.Prefix`、S3 は `CommonPrefixes`）のように壊れても静かなロジックがテストの外にありました。
* `remoteio` パッケージ側のテストは、ローカルハンドラと `Router` の振り分けに集中します。
* `remoteio/gcs` のファクトリテストのうち、引数なしの `New(ctx)` を通るものは `storage.NewClient` が ADC を要求するため、`google.FindDefaultCredentials` が失敗する環境 (CI 等) では `skipWithoutGCPCredentials` でスキップします。`gcs.WithClient` でフェイクのクライアントを注入するテスト (`factory_options_test.go`) はどこでも走ります。
* `remoteio/multi_integration_test.go` は `package remoteio_test`（外部テストパッケージ）で、GCS と S3 の両フェイクを 1 つの `Bundle` に束ねて検証します。外部テストパッケージなので、`remoteio` 本体の依存にクラウド SDK は入りません。
* インターフェース充足は `TestXxx_InterfaceSatisfaction` (コンパイル時チェック) と各ファクトリの `var _ remoteio.IOFactory = (*ClientFactory)(nil)` で担保。

## コミット / ブランチ

Conventional Commits (`feat(remoteio):`, `fix:`, `chore(deps):`, `test:`, `refactor:`, `docs:`)。CI は `main` と `develop` への push / PR で走ります。作業ブランチは `develop`、PR 先は通常 `main`。
