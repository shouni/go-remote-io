package remoteio

import (
	"context"
	"errors"
	"io"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// collect は反復子を読み切って Entry を集めます。
func collect(t *testing.T, seq iter.Seq2[Entry, error]) []Entry {
	t.Helper()
	var out []Entry
	for entry, err := range seq {
		require.NoError(t, err)
		out = append(out, entry)
	}
	return out
}

// firstErr は反復子から最初のエラーを取り出します。
func firstErr(t *testing.T, seq iter.Seq2[Entry, error]) error {
	t.Helper()
	for _, err := range seq {
		if err != nil {
			return err
		}
	}
	return nil
}

// names は Entry の Name を並べます。
func names(entries []Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name)
	}
	return out
}

func TestRouterResolve(t *testing.T) {
	ctx := context.Background()
	store := NewStore()

	t.Run("未登録スキームは ErrUnsupportedScheme", func(t *testing.T) {
		_, err := store.Open(ctx, "gs://bucket/key")
		assert.ErrorIs(t, err, ErrUnsupportedScheme)
		assert.Contains(t, err.Error(), "gs", "どのスキームだったかがメッセージに残ること")
	})

	t.Run("ローカルとfileは既定で登録される", func(t *testing.T) {
		assert.Equal(t, []string{SchemeFile}, store.Schemes(), "フォールバックは Schemes に含めない")

		dir := t.TempDir()
		path := filepath.Join(dir, "a.txt")
		require.NoError(t, os.WriteFile(path, []byte("hello"), 0o644))

		data, err := ReadAll(ctx, store, path)
		require.NoError(t, err)
		assert.Equal(t, "hello", string(data))

		data, err = ReadAll(ctx, store, "file://"+filepath.ToSlash(path))
		require.NoError(t, err)
		assert.Equal(t, "hello", string(data))
	})

	t.Run("フォールバックが無ければローカルパスも拒否する", func(t *testing.T) {
		bare := NewRouter(NewFileHandler())
		_, err := bare.Open(ctx, "a.txt")
		assert.ErrorIs(t, err, ErrUnsupportedScheme)
	})
}

// Exists の意味をスキーム間で揃えたことの確認です。
// v1 はローカルだけディレクトリに true を返していました。
func TestExistsIsAboutObjectsOnly(t *testing.T) {
	ctx := context.Background()
	store := NewStore()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o755))

	t.Run("ファイルは true", func(t *testing.T) {
		ok, err := store.Exists(ctx, filepath.Join(dir, "a.txt"))
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("不在は (false, nil)", func(t *testing.T) {
		ok, err := store.Exists(ctx, filepath.Join(dir, "missing.txt"))
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("ディレクトリは false", func(t *testing.T) {
		ok, err := store.Exists(ctx, filepath.Join(dir, "sub"))
		require.NoError(t, err)
		assert.False(t, ok, "リモートにディレクトリという実体は無いので揃える")
	})

	t.Run("Stat もディレクトリには ErrNotExist", func(t *testing.T) {
		_, err := store.Stat(ctx, filepath.Join(dir, "sub"))
		assert.ErrorIs(t, err, ErrNotExist)
	})
}

func TestSubScoping(t *testing.T) {
	ctx := context.Background()
	store := NewStore()
	dir := t.TempDir()

	jobs := store.Sub(filepath.Join(dir, "jobs"))

	t.Run("相対名がスコープ内へ解決される", func(t *testing.T) {
		require.NoError(t, WriteAll(ctx, jobs, "j1/status.json", []byte(`{"state":"queued"}`)))

		// ルートのストアから絶対パスで読めることで、結合が意図どおりか分かります。
		data, err := ReadAll(ctx, store, filepath.Join(dir, "jobs", "j1", "status.json"))
		require.NoError(t, err)
		assert.JSONEq(t, `{"state":"queued"}`, string(data))
	})

	t.Run("入れ子の Sub は積み上がる", func(t *testing.T) {
		j1 := jobs.Sub("j1")
		data, err := ReadAll(ctx, j1, "status.json")
		require.NoError(t, err)
		assert.JSONEq(t, `{"state":"queued"}`, string(data))
	})

	t.Run("絶対URIは ErrAbsoluteName で拒否する", func(t *testing.T) {
		// スコープを絞ったつもりのコードが別のバケットへ書けてしまうのを防ぎます。
		err := WriteAll(ctx, jobs, "gs://other-bucket/x.json", []byte("{}"))
		assert.ErrorIs(t, err, ErrAbsoluteName)

		_, err = jobs.Open(ctx, "gs://other-bucket/x.json")
		assert.ErrorIs(t, err, ErrAbsoluteName)

		assert.ErrorIs(t, firstErr(t, jobs.List(ctx, "s3://other/x")), ErrAbsoluteName)
	})

	t.Run("Entry.Name はスコープ付きストアへそのまま渡せる", func(t *testing.T) {
		require.NoError(t, WriteAll(ctx, jobs, "j2/status.json", []byte(`{}`)))

		var found bool
		for entry, err := range jobs.List(ctx, "", WithDelimiter("/")) {
			require.NoError(t, err)
			if !entry.IsPrefix {
				continue
			}
			// 疑似ディレクトリ名から、そのまま次のスコープを作れます。
			sub := jobs.Sub(strings.TrimSuffix(entry.Name, "/"))
			if ok, _ := sub.Exists(ctx, "status.json"); ok {
				found = true
			}
		}
		assert.True(t, found)
	})
}

func TestListLocal(t *testing.T) {
	ctx := context.Background()
	store := NewStore()
	dir := t.TempDir()

	write := func(rel string) {
		t.Helper()
		full := filepath.Join(dir, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte("x"), 0o644))
	}
	write("data/a.txt")
	write("data/sub/b.txt")
	write("data-archive/c.txt")

	t.Run("区切り文字ありは直下のみ、疑似ディレクトリは IsPrefix", func(t *testing.T) {
		entries := collect(t, store.List(ctx, filepath.Join(dir, "data"), WithDelimiter("/")))
		assert.ElementsMatch(t, []string{"a.txt", "sub/"}, names(entries))

		for _, e := range entries {
			if e.Name == "sub/" {
				assert.True(t, e.IsPrefix, "呼び出し側が末尾を見て判定しなくて済むこと")
			} else {
				assert.False(t, e.IsPrefix)
				assert.Equal(t, int64(1), e.Size)
			}
		}
	})

	t.Run("区切り文字なしは再帰的にファイルのみ", func(t *testing.T) {
		entries := collect(t, store.List(ctx, filepath.Join(dir, "data")))
		assert.ElementsMatch(t, []string{"a.txt", "sub/b.txt"}, names(entries))
		for _, e := range entries {
			assert.False(t, e.IsPrefix)
		}
	})

	// v1 は区切り文字を指定しないとき素の前方一致だったため、
	// "data" が "data-archive/" まで拾っていました。
	t.Run("プレフィックスは常に正規化される", func(t *testing.T) {
		entries := collect(t, store.List(ctx, filepath.Join(dir, "data")))
		for _, e := range entries {
			assert.NotContains(t, e.URI, "data-archive", "隣接する名前を拾わないこと")
		}
	})

	t.Run("break で打ち切れる", func(t *testing.T) {
		var seen int
		for _, err := range store.List(ctx, dir) {
			require.NoError(t, err)
			seen++
			break
		}
		assert.Equal(t, 1, seen)
	})

	t.Run("不在ディレクトリは ErrNotExist", func(t *testing.T) {
		err := firstErr(t, store.List(ctx, filepath.Join(dir, "missing")))
		assert.ErrorIs(t, err, ErrNotExist)
	})
}

// failingReader は途中まで読めたあとに失敗するリーダーです。
type failingReader struct{ r io.Reader }

func (f *failingReader) Read(p []byte) (int, error) {
	if n, _ := f.r.Read(p); n > 0 {
		return n, nil
	}
	return 0, errors.New("読み取り失敗")
}

// 「成功しなければ書き込み先は変化しない」という Handler の契約の確認です。
func TestWriteIsAtomic(t *testing.T) {
	ctx := context.Background()
	store := NewStore()
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")

	require.NoError(t, os.WriteFile(path, []byte("original"), 0o600))

	err := store.Write(ctx, path, &failingReader{r: strings.NewReader("partial-data")})
	require.Error(t, err)

	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, "original", string(data), "失敗した書き込みが既存の内容を壊してはいけない")

	t.Run("一時ファイルを残さない", func(t *testing.T) {
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		for _, e := range entries {
			assert.NotContains(t, e.Name(), ".tmp-", "失敗経路でも後始末される")
		}
	})

	t.Run("既存ファイルのパーミッションを引き継ぐ", func(t *testing.T) {
		require.NoError(t, WriteAll(ctx, store, path, []byte("replaced")))
		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	})
}

func TestWriteIfNotExists(t *testing.T) {
	ctx := context.Background()
	store := NewStore()
	dir := t.TempDir()
	path := filepath.Join(dir, "once.txt")

	require.NoError(t, WriteAll(ctx, store, path, []byte("first"), WithIfNotExists()))

	err := WriteAll(ctx, store, path, []byte("second"), WithIfNotExists())
	assert.ErrorIs(t, err, ErrExist)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "first", string(data), "既存の内容が保たれること")
}

func TestWriteRespectsContextCancellation(t *testing.T) {
	store := NewStore()
	dir := t.TempDir()
	path := filepath.Join(dir, "cancelled.txt")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := store.Write(ctx, path, strings.NewReader("x"))
	assert.ErrorIs(t, err, context.Canceled)

	_, statErr := os.Stat(path)
	assert.ErrorIs(t, statErr, os.ErrNotExist, "キャンセル時にファイルを作らないこと")
}

func TestCopyAndMoveLocal(t *testing.T) {
	ctx := context.Background()
	store := NewStore()
	dir := t.TempDir()

	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	require.NoError(t, WriteAll(ctx, store, src, []byte("payload")))

	t.Run("Copy はコピー元を残す", func(t *testing.T) {
		require.NoError(t, store.Copy(ctx, src, dst))

		data, err := ReadAll(ctx, store, dst)
		require.NoError(t, err)
		assert.Equal(t, "payload", string(data))

		ok, err := store.Exists(ctx, src)
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("Move はコピー成功後にコピー元を消す", func(t *testing.T) {
		moved := filepath.Join(dir, "moved.txt")
		require.NoError(t, Move(ctx, store, src, moved))

		ok, err := store.Exists(ctx, src)
		require.NoError(t, err)
		assert.False(t, ok)

		data, err := ReadAll(ctx, store, moved)
		require.NoError(t, err)
		assert.Equal(t, "payload", string(data))
	})

	t.Run("コピー元が無ければコピー先を作らない", func(t *testing.T) {
		err := store.Copy(ctx, filepath.Join(dir, "missing.txt"), filepath.Join(dir, "never.txt"))
		assert.ErrorIs(t, err, ErrNotExist)

		ok, err := store.Exists(ctx, filepath.Join(dir, "never.txt"))
		require.NoError(t, err)
		assert.False(t, ok)
	})
}

func TestDeleteIsIdempotent(t *testing.T) {
	ctx := context.Background()
	store := NewStore()
	path := filepath.Join(t.TempDir(), "gone.txt")

	require.NoError(t, WriteAll(ctx, store, path, []byte("x")))
	require.NoError(t, store.Delete(ctx, path))
	assert.NoError(t, store.Delete(ctx, path), "不在の削除はエラーにしない")
}

// 署名器を持たないハンドラへ要求したときは ErrNotSupported で返します。
func TestSignURLUnsupportedForLocal(t *testing.T) {
	store := NewStore()
	_, err := store.SignURL(context.Background(), filepath.Join(t.TempDir(), "a.txt"), "GET", time.Minute)
	assert.ErrorIs(t, err, ErrNotSupported)
}

func TestFileHandlerRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := NewStore()
	dir := t.TempDir()

	uri := "file://" + filepath.ToSlash(filepath.Join(dir, "a.txt"))
	require.NoError(t, WriteAll(ctx, store, uri, []byte("via file scheme")))

	t.Run("スキームなしのパスからも同じ内容が読める", func(t *testing.T) {
		data, err := ReadAll(ctx, store, filepath.Join(dir, "a.txt"))
		require.NoError(t, err)
		assert.Equal(t, "via file scheme", string(data))
	})

	t.Run("Stat の URI は問い合わせた形に戻る", func(t *testing.T) {
		info, err := store.Stat(ctx, uri)
		require.NoError(t, err)
		assert.Equal(t, uri, info.URI)
	})

	t.Run("List は file:// の URI を返す", func(t *testing.T) {
		entries := collect(t, store.List(ctx, "file://"+filepath.ToSlash(dir)))
		require.Len(t, entries, 1)
		assert.True(t, strings.HasPrefix(entries[0].URI, "file://"), "次の呼び出しへそのまま渡せる形であること")
	})

	t.Run("パーセントエンコードをデコードする", func(t *testing.T) {
		spaced := filepath.Join(dir, "a b.txt")
		require.NoError(t, os.WriteFile(spaced, []byte("spaced"), 0o644))

		data, err := ReadAll(ctx, store, "file://"+filepath.ToSlash(dir)+"/a%20b.txt")
		require.NoError(t, err)
		assert.Equal(t, "spaced", string(data))
	})

	t.Run("file:// でない URI は ErrInvalidURI", func(t *testing.T) {
		_, err := NewFileHandler().Open(ctx, "gs://b/k")
		assert.ErrorIs(t, err, ErrInvalidURI)
	})
}
