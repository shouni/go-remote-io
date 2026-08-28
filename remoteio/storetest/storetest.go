// Package storetest は、remoteio.Handler の実装が契約を満たしているかを検査する
// 適合性テストスイートを提供します。
//
// 使い方は testing/fstest の TestFS と同じ発想です。GCS / S3 / ローカル /
// memio / 第三者の実装が、すべて同じ 1 本のスイートを通ります。
//
//	func TestConformance(t *testing.T) {
//		storetest.TestHandler(t, func(t *testing.T) storetest.Fixture {
//			return storetest.Fixture{Handler: memio.New(), Root: "mem://bucket/conformance"}
//		})
//	}
//
// これが無いと、フェイクと本物がずれても CI は緑のままになります。実際
// 「区切り文字なしの一覧がスキームによって別の意味になる」「ローカルだけ
// ディレクトリを Exists で true にする」といった食い違いが、長いあいだ
// テストの外にありました。
package storetest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/shouni/go-remote-io/remoteio"
)

// Fixture は、スイートが 1 つの Handler を試すために必要な足場です。
type Fixture struct {
	// Handler は検査対象です。
	Handler remoteio.Handler

	// Root は、スイートが自由に読み書きしてよい場所です。
	// リモートなら "gs://bucket/conformance"、ローカルなら t.TempDir() の値を渡します。
	// スイートはこの下にしか触りません。
	Root string

	// SupportsContentType は、Content-Type を保存して Stat で返せるかどうかです。
	// ローカルファイルシステムは保持できないため false になります。
	SupportsContentType bool

	// SupportsMetadata は、ユーザー定義メタデータを保存して Stat で返せるかどうかです。
	SupportsMetadata bool

	// SupportsIfNotExists は、条件付き書き込みに対応しているかどうかです。
	// フェイクが条件付きリクエストを解釈しない場合は false にしてください
	// （本物との差なので、黙って通すのではなく宣言します）。
	SupportsIfNotExists bool

	// BucketScoped は、URI が「スキーム + バケット + キー」の形かどうかです。
	// gs:// / s3:// は true、file:// はスキームを持ちますがバケットの概念が無いため
	// false です。「オブジェクト名が空の URI を拒否する」検査の対象を分けます。
	BucketScoped bool
}

// TestHandler は Handler の契約を検査します。
//
// newFixture はサブテストごとに呼ばれます。1 つのサブテストが書いたものが
// 次のサブテストへ漏れないよう、毎回まっさらな足場を返してください。
func TestHandler(t *testing.T, newFixture func(t *testing.T) Fixture) {
	t.Helper()

	run := func(name string, fn func(t *testing.T, f Fixture)) {
		t.Run(name, func(t *testing.T) {
			fn(t, newFixture(t))
		})
	}

	run("Scheme は呼ぶたびに同じ値を返す", testSchemeIsStable)
	run("不在は ErrNotExist を含む", testNotExist)
	run("書いた内容を読み戻せる", testWriteReadRoundTrip)
	run("Stat がサイズと URI を返す", testStatMetadata)
	run("Delete は冪等", testDeleteIsIdempotent)
	run("区切り文字ありの一覧は直下のみ", testListShallow)
	run("区切り文字なしの一覧は再帰的", testListRecursive)
	run("一覧のプレフィックスは常に正規化される", testListNormalizesPrefix)
	run("何も無いプレフィックスの一覧は空でエラーにしない", testListEmptyPrefix)
	run("一覧は break で打ち切れる", testListStops)
	run("Entry.URI はそのまま開ける", testEntryURIIsUsable)
	run("失敗した書き込みは書き込み先を変えない", testWriteIsAtomic)
	run("条件付き書き込みは既存を守る", testWriteIfNotExists)
	run("担当外のスキームを拒否する", testRejectsForeignScheme)
	run("オブジェクト名が空の URI を拒否する", testRejectsEmptyObjectName)
}

// uri は Root からの相対名を、ハンドラが受け取る形へ組み立てます。
func (f Fixture) uri(name string) string { return remoteio.Join(f.Root, name) }

// write は前提を組み立てるための書き込みです。
func (f Fixture) write(t *testing.T, name, content string) string {
	t.Helper()
	full := f.uri(name)
	err := f.Handler.Write(context.Background(), full, strings.NewReader(content),
		remoteio.WriteOptions{ContentType: remoteio.DefaultContentType})
	if err != nil {
		t.Fatalf("前提の書き込みに失敗しました (%s): %v", full, err)
	}
	return full
}

// read はハンドラから内容を読み切ります。
func (f Fixture) read(t *testing.T, uri string) string {
	t.Helper()
	rc, err := f.Handler.Open(context.Background(), uri)
	if err != nil {
		t.Fatalf("Open に失敗しました (%s): %v", uri, err)
	}
	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("読み込みに失敗しました (%s): %v", uri, err)
	}
	return string(data)
}

// collect は一覧を読み切ります。
func (f Fixture) collect(t *testing.T, uri string, opts remoteio.ListOptions) []remoteio.Entry {
	t.Helper()
	var out []remoteio.Entry
	for entry, err := range f.Handler.List(context.Background(), uri, opts) {
		if err != nil {
			t.Fatalf("List に失敗しました (%s): %v", uri, err)
		}
		out = append(out, entry)
	}
	return out
}

func names(entries []remoteio.Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name)
	}
	return out
}

func testSchemeIsStable(t *testing.T, f Fixture) {
	if got, want := f.Handler.Scheme(), f.Handler.Scheme(); got != want {
		t.Fatalf("Scheme() が呼ぶたびに変わります: %q と %q", got, want)
	}
	if strings.Contains(f.Handler.Scheme(), "://") {
		t.Errorf("Scheme() は区切りを含まない名前を返してください: %q", f.Handler.Scheme())
	}
}

// 呼び出し側がスキームに依らず errors.Is で不在を判定できることが、
// この抽象が成立するための条件です。
func testNotExist(t *testing.T, f Fixture) {
	ctx := context.Background()
	missing := f.uri("missing.txt")

	if _, err := f.Handler.Open(ctx, missing); !errors.Is(err, remoteio.ErrNotExist) {
		t.Errorf("Open の不在エラーが ErrNotExist を含みません: %v", err)
	}
	if _, err := f.Handler.Stat(ctx, missing); !errors.Is(err, remoteio.ErrNotExist) {
		t.Errorf("Stat の不在エラーが ErrNotExist を含みません: %v", err)
	}
}

func testWriteReadRoundTrip(t *testing.T, f Fixture) {
	uri := f.write(t, "round/trip.txt", "hello")
	if got := f.read(t, uri); got != "hello" {
		t.Errorf("読み戻した内容が違います: got %q, want %q", got, "hello")
	}
}

func testStatMetadata(t *testing.T, f Fixture) {
	ctx := context.Background()
	uri := f.uri("meta.txt")

	opts := remoteio.WriteOptions{ContentType: "application/json", Metadata: map[string]string{"job-id": "j1"}}
	if err := f.Handler.Write(ctx, uri, strings.NewReader(`{"a":1}`), opts); err != nil {
		t.Fatalf("Write に失敗しました: %v", err)
	}

	info, err := f.Handler.Stat(ctx, uri)
	if err != nil {
		t.Fatalf("Stat に失敗しました: %v", err)
	}
	if info.URI != uri {
		t.Errorf("ObjectInfo.URI は問い合わせに使った URI を返してください: got %q, want %q", info.URI, uri)
	}
	if info.Size != int64(len(`{"a":1}`)) {
		t.Errorf("Size が違います: got %d, want %d", info.Size, len(`{"a":1}`))
	}
	if f.SupportsContentType && info.ContentType != "application/json" {
		t.Errorf("ContentType が保存されていません: got %q", info.ContentType)
	}
	if f.SupportsMetadata && info.Metadata["job-id"] != "j1" {
		t.Errorf("Metadata が保存されていません: got %v", info.Metadata)
	}
}

func testDeleteIsIdempotent(t *testing.T, f Fixture) {
	ctx := context.Background()
	uri := f.write(t, "delete-me.txt", "x")

	if err := f.Handler.Delete(ctx, uri); err != nil {
		t.Fatalf("Delete に失敗しました: %v", err)
	}
	if err := f.Handler.Delete(ctx, uri); err != nil {
		t.Errorf("不在の Delete はエラーにしないでください: %v", err)
	}
	if _, err := f.Handler.Stat(ctx, uri); !errors.Is(err, remoteio.ErrNotExist) {
		t.Errorf("削除後の Stat が ErrNotExist を返しません: %v", err)
	}
}

// 疑似ディレクトリが IsPrefix として渡ることを確かめます。
// これが文字列に潰れていると、呼び出し側が末尾の区切り文字を見て判定し直す形に戻ります。
func testListShallow(t *testing.T, f Fixture) {
	f.write(t, "data/a.txt", "a")
	f.write(t, "data/sub/b.txt", "b")

	entries := f.collect(t, f.uri("data"), remoteio.ListOptions{Delimiter: "/"})
	got := names(entries)
	if len(got) != 2 {
		t.Fatalf("直下は 2 件のはずです: got %v", got)
	}

	var sawFile, sawPrefix bool
	for _, e := range entries {
		switch e.Name {
		case "a.txt":
			sawFile = true
			if e.IsPrefix {
				t.Errorf("オブジェクトが IsPrefix になっています: %+v", e)
			}
		case "sub/":
			sawPrefix = true
			if !e.IsPrefix {
				t.Errorf("疑似ディレクトリが IsPrefix になっていません: %+v", e)
			}
		default:
			t.Errorf("想定外の Entry: %+v", e)
		}
	}
	if !sawFile || !sawPrefix {
		t.Errorf("直下のオブジェクトと疑似ディレクトリの両方が要ります: got %v", got)
	}
}

func testListRecursive(t *testing.T, f Fixture) {
	f.write(t, "data/a.txt", "a")
	f.write(t, "data/sub/b.txt", "b")

	entries := f.collect(t, f.uri("data"), remoteio.ListOptions{})
	got := names(entries)
	if len(got) != 2 {
		t.Fatalf("配下を再帰的に返してください: got %v", got)
	}
	for _, e := range entries {
		if e.IsPrefix {
			t.Errorf("区切り文字なしの一覧に疑似ディレクトリが混ざっています: %+v", e)
		}
	}
	// Name は列挙したプレフィックスからの相対名です。
	if !slicesContains(got, "a.txt") || !slicesContains(got, "sub/b.txt") {
		t.Errorf("Name はプレフィックスからの相対名にしてください: got %v", got)
	}
}

// 正規化しないと素の前方一致になり、"data" が "data-archive/" にも一致します。
// スキームによって意味が変わらないよう、実装は常に正規化しなければなりません。
func testListNormalizesPrefix(t *testing.T, f Fixture) {
	f.write(t, "data/a.txt", "a")
	f.write(t, "data-archive/c.txt", "c")

	for _, opts := range []remoteio.ListOptions{{}, {Delimiter: "/"}} {
		for _, e := range f.collect(t, f.uri("data"), opts) {
			if strings.Contains(e.URI, "data-archive") {
				t.Errorf("隣接する名前を拾っています (delimiter=%q): %+v", opts.Delimiter, e)
			}
		}
	}
}

// リモートに「存在しないプレフィックス」という状態はありません。
// ローカルだけエラーにすると、同じ呼び出しがスキームによって別の意味になります。
func testListEmptyPrefix(t *testing.T, f Fixture) {
	for entry, err := range f.Handler.List(context.Background(), f.uri("nothing-here"), remoteio.ListOptions{}) {
		if err != nil {
			t.Fatalf("何も無いプレフィックスの一覧はエラーにしないでください: %v", err)
		}
		t.Errorf("何も無いはずのプレフィックスから Entry が返りました: %+v", entry)
	}
}

func testListStops(t *testing.T, f Fixture) {
	f.write(t, "data/a.txt", "a")
	f.write(t, "data/b.txt", "b")
	f.write(t, "data/c.txt", "c")

	var seen int
	for _, err := range f.Handler.List(context.Background(), f.uri("data"), remoteio.ListOptions{}) {
		if err != nil {
			t.Fatalf("List に失敗しました: %v", err)
		}
		seen++
		break
	}
	if seen != 1 {
		t.Errorf("break で打ち切れていません: %d 件処理されました", seen)
	}
}

// Entry.URI はルートの Store へそのまま渡せる完全な形でなければなりません。
func testEntryURIIsUsable(t *testing.T, f Fixture) {
	f.write(t, "data/a.txt", "payload")

	entries := f.collect(t, f.uri("data"), remoteio.ListOptions{})
	if len(entries) == 0 {
		t.Fatal("一覧が空です")
	}
	if got := f.read(t, entries[0].URI); got != "payload" {
		t.Errorf("Entry.URI から読めた内容が違います: got %q", got)
	}
}

// failingReader は途中まで読めたあとに失敗するリーダーです。
type failingReader struct{ r io.Reader }

func (f *failingReader) Read(p []byte) (int, error) {
	if n, _ := f.r.Read(p); n > 0 {
		return n, nil
	}
	return 0, errors.New("storetest: 読み取り失敗")
}

// 「成功しなければ書き込み先が変化しない」という契約の検査です。
func testWriteIsAtomic(t *testing.T, f Fixture) {
	ctx := context.Background()

	t.Run("新規", func(t *testing.T) {
		uri := f.uri("atomic/new.txt")
		src := &failingReader{r: bytes.NewReader([]byte("partial-data"))}
		if err := f.Handler.Write(ctx, uri, src, remoteio.WriteOptions{}); err == nil {
			t.Fatal("失敗するはずの書き込みが成功しました")
		}
		if _, err := f.Handler.Stat(ctx, uri); !errors.Is(err, remoteio.ErrNotExist) {
			t.Errorf("失敗した書き込みでオブジェクトが残りました: %v", err)
		}
	})

	t.Run("上書き", func(t *testing.T) {
		uri := f.write(t, "atomic/existing.txt", "original")
		src := &failingReader{r: bytes.NewReader([]byte("partial-data"))}
		if err := f.Handler.Write(ctx, uri, src, remoteio.WriteOptions{}); err == nil {
			t.Fatal("失敗するはずの書き込みが成功しました")
		}
		if got := f.read(t, uri); got != "original" {
			t.Errorf("失敗した上書きが既存の内容を壊しました: got %q", got)
		}
	})
}

func testWriteIfNotExists(t *testing.T, f Fixture) {
	if !f.SupportsIfNotExists {
		t.Skip("この実装は条件付き書き込みに対応していません")
	}
	ctx := context.Background()
	uri := f.uri("once.txt")

	opts := remoteio.WriteOptions{ContentType: remoteio.DefaultContentType, IfNotExists: true}
	if err := f.Handler.Write(ctx, uri, strings.NewReader("first"), opts); err != nil {
		t.Fatalf("初回の書き込みに失敗しました: %v", err)
	}
	if err := f.Handler.Write(ctx, uri, strings.NewReader("second"), opts); !errors.Is(err, remoteio.ErrExist) {
		t.Fatalf("既存への条件付き書き込みが ErrExist を返しません: %v", err)
	}
	if got := f.read(t, uri); got != "first" {
		t.Errorf("既存の内容が壊れました: got %q", got)
	}
}

// ハンドラは公開されているため、Router を通さずに直接呼ばれる余地があります。
// 担当外のスキームを黙って処理すると、別のクラウドのバケットを触ります。
func testRejectsForeignScheme(t *testing.T, f Fixture) {
	if f.Handler.Scheme() == "" {
		t.Skip("フォールバックのハンドラはスキームを持ちません")
	}
	foreign := "definitely-not-" + f.Handler.Scheme() + "://bucket/key"
	if _, err := f.Handler.Open(context.Background(), foreign); !errors.Is(err, remoteio.ErrInvalidURI) {
		t.Errorf("担当外のスキームが ErrInvalidURI で拒否されません: %v", err)
	}
}

// バケット操作と取り違えたり、不在なのか URI 不正なのか区別できなくなるのを防ぎます。
func testRejectsEmptyObjectName(t *testing.T, f Fixture) {
	if !f.BucketScoped {
		t.Skip("この実装にバケットの概念はありません")
	}
	_, bucket, _, err := remoteio.ParseURI(f.Root)
	if err != nil {
		t.Fatalf("Fixture.Root が URI として解釈できません (%s): %v", f.Root, err)
	}

	bucketOnly := remoteio.BuildURI(f.Handler.Scheme(), bucket, "")
	if _, err := f.Handler.Open(context.Background(), bucketOnly); !errors.Is(err, remoteio.ErrInvalidURI) {
		t.Errorf("オブジェクト名が空の URI が拒否されません: %v", err)
	}
}

func slicesContains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
