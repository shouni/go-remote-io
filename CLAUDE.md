# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## このリポジトリについて

`github.com/shouni/go-remote-io` は GCS / S3 / ローカルファイルシステムを単一のインターフェースで扱うライブラリです。実行バイナリ (`main` パッケージ) は無く、公開 API がそのまま成果物です。タグ付きリリース (現在 v1.7.0 系) を打っているため、**エクスポートされた識別子の変更・削除は破壊的変更**として扱ってください。

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
go test -run 'TestUniversalInputReader_Local/List: stops and returns callback error' ./remoteio/
```

`golangci-lint` / `govulncheck` はローカルにインストール済み (`~/go/bin`)。

## アーキテクチャ

### スキーム振り分けは「呼び出し時」に行われる

`remoteio` パッケージの具象型は `UniversalInputReader` と `UniversalIOWriter` の 2 つだけで、どちらも GCS クライアントと S3 クライアントの**両方**を保持します (`NewUniversalInputReader(gcsClient, s3Client)`)。実際にどのストレージを触るかは、渡された `path` の接頭辞 (`gs://` / `s3://` / それ以外はローカル) を毎回判定して決まります。

* `remoteio/gcs`・`remoteio/s3` のサブパッケージは「クライアントを 1 つだけ注入した Universal 型」を組み立てる `ClientFactory` にすぎません。したがって **gcs ファクトリから得た Reader でもローカルパスは読める**一方、`s3://` を渡すと「S3クライアントが未初期化です」エラーになります。
* 注入されていないクライアント側の分岐は必ず nil ガードから始まります。新しいストレージ操作を追加するときは、この nil ガード → URI パース → 空オブジェクト名チェックの順序を既存実装に合わせてください。
* Writer 側の分岐は `dispatchRemote` (`remoteio/writer.go`) に一本化されています。`Write` と `Delete` が同形の分岐を持っていた重複を潰したものなので、新しい書き込み系操作もここを通します。Reader 側は戻り値の型が揃わないため個別の if 連鎖のままです。

### ファイル分割の規則

`reader.go` / `writer.go` が振り分けとエントリポイントを持ち、実装は `*_local.go` / `*_gcs.go` / `*_s3.go` に非公開メソッド (`openGCS`, `listS3`, `writeLocal` …) として置かれます。ストレージ固有の処理を `reader.go` や `writer.go` に書かないこと。

### 2 系統の Functional Options

| | 設定型 | 公開範囲 |
| :-- | :-- | :-- |
| `WriteOption` (`write_options.go`) | `writeConfig` | 非公開 |
| `ListOption` (`list_options.go`) | `ListSettings` | **公開** |

`ListSettings` と `NewListSettings` / `ListPrefix` を公開しているのは意図的です。`Lister` インターフェース経由で `opts ...ListOption` を受け取る第三者実装やテストのフェイクが、中身を読めないまま区切り文字を無視してしまう状態を避けるためです (commit ed05c68)。`ListOption` を増やす場合は `ListSettings` にフィールドを足し、実装側が `NewListSettings(opts...)` で同じ解決結果を得られる形を保ちます。

`WriteOption` の `Content-Type` などのメタデータはローカル書き込みでは黙って無視されます (`writer_local.go` のコメント参照)。

### `WithDelimiter` の意味論

区切り文字を渡すと prefix 直下だけが対象になり、疑似ディレクトリが**区切り文字で終わる URI** として callback に渡されます。呼び出し側は末尾で判別します。実装上の要点:

* prefix は `ListPrefix` で正規化される (`music` → `music/`)。正規化しないと `music-archive/` まで一致するため。
* GCS は疑似ディレクトリが `attrs.Name` 空 + `attrs.Prefix` に、S3 は `Contents` ではなく `CommonPrefixes` に入る。prefix 自身は除外する。
* ローカルの `listLocal` は、**区切り文字が指定されたときだけ**ディレクトリを列挙します。未指定時にディレクトリを混ぜると既存呼び出し側の挙動が変わるため、後方互換のためのガードです。

### エラーの約束事

* `Open` の「見つからない」は `os.ErrNotExist` を `%w` でラップして返す (GCS/S3 とも)。呼び出し側が `errors.Is` でスキーム非依存に判定できるようにするため。
* `Exists` は不在を `(false, nil)` で返し、それ以外の失敗のみエラーにする。
* `Open` / `Exists` はオブジェクト名が空の URI (`gs://bucket` など) を明示的に拒否する。バケット操作と取り違えたり、不在なのか URI 不正なのか区別できなくなるのを防ぐため。
* S3 の型付きエラー判定は `errors.AsType[*types.NoSuchKey](err)` を使用 (Go 1.26 の `errors.AsType`)。`HeadObject` は `NotFound` も返すため両方見る。
* ローカル書き込みは `io.Copy` に `ctxReader` を挟んで ctx キャンセルを検知します。`os.File` への `io.Copy` は ctx を見ないため、これを外すと巨大ファイルを最後まで書き切ってしまいます。

### ファクトリと署名付き URL

* `IOFactory` は `Close` / `InputReader` / `OutputWriter` / `URLSigner` を要求。両ファクトリはインターフェース外の `Reader()` / `Writer()` も持ち、複合インターフェース側へ委譲しています。
* `remoteio.Bundle` (`bundle.go`) は、その 3 アクセサを一度に取り出して保持する構造体です。利用側が全く同じ組み立て関数と構造体を各自持っていたものを引き取ったもので、**所有権の受け渡しが要点**です。`NewBundle` は成功した場合にのみ factory のライフサイクルを引き取り（以降 `Bundle.Close` が閉じる）、失敗時は閉じずに返します。組み立て途中の後始末を、他の資源とまとめて呼び出し元が行えるようにするためです。`Close` が nil レシーバーを許容するのは `[]io.Closer` へ入れて一括解放される使われ方に備えたもの。
* `Bundle` を `Client` と呼ばないのは粒度が違うためです。この型は接続を持たず I/O もせず、他が実装したインターフェースを束ねているだけで、`Close` 以外のメソッドを持ちません。
* `Close` 後はクライアントを nil にし、以降のアクセサはエラーを返す (S3 の `Close` は SDK 的には不要だが同じ契約に揃えている)。`Close` は冪等。
* 署名付き URL はスキーム厳格: `gcsURLSigner` は `gs://` 以外、`s3URLSigner` は `s3://` 以外を拒否。S3 は GET / PUT のみ対応。
* S3 のリージョン未設定時のデフォルトは `ap-northeast-1` (`remoteio/s3/factory.go`)。

## テスト

* testify (`require` で前提、`assert` で検証) + `t.Run` サブテスト。テーブル駆動が基本。
* クラウドのエミュレータは使いません。リモートパスのテストは **nil クライアントを渡してバリデーション・振り分けのエラーを確認する**範囲に留めています。実 I/O を伴うテストを足すときはこの前提を崩さないか検討してください。
* `remoteio/gcs` のファクトリテストは `storage.NewClient` が ADC を要求するため、`google.FindDefaultCredentials` が失敗する環境 (CI 等) では `skipWithoutGCPCredentials` でスキップします。
* インターフェース充足は `TestXxx_InterfaceSatisfaction` (コンパイル時チェック) と各ファクトリの `var _ remoteio.IOFactory = (*ClientFactory)(nil)` で担保。

## コミット / ブランチ

Conventional Commits (`feat(remoteio):`, `fix:`, `chore(deps):`, `test:`, `refactor:`, `docs:`)。CI は `main` と `develop` への push / PR で走ります。作業ブランチは `develop`、PR 先は通常 `main`。
