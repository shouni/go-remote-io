package memio_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-remote-io/remoteio/memio"
)

func uri(name string) string {
	return remoteio.BuildURI(memio.DefaultScheme, "bucket", name)
}

// Seed / URIs / Len は、利用側のテストが前提を組み立てて結果を確かめるための入口です。
func TestSeedAndInspect(t *testing.T) {
	ctx := context.Background()
	h := memio.New()

	require.NoError(t, h.Seed(uri("jobs/j1/status.json"), []byte(`{"state":"queued"}`)))
	require.NoError(t, h.Seed(uri("jobs/j2/status.json"), []byte(`{"state":"done"}`)))

	assert.Equal(t, 2, h.Len())
	assert.Equal(t, []string{uri("jobs/j1/status.json"), uri("jobs/j2/status.json")}, h.URIs(),
		"URIs は辞書順で安定していること")

	store := remoteio.NewStore(h)
	data, err := remoteio.ReadAll(ctx, store, uri("jobs/j1/status.json"))
	require.NoError(t, err)
	assert.JSONEq(t, `{"state":"queued"}`, string(data))
}

// 保存した中身が呼び出し側から書き換えられないことの確認です。
// ここが共有だと、テストが互いの状態を壊し合います。
func TestStoredDataIsCopied(t *testing.T) {
	ctx := context.Background()
	h := memio.New()

	original := []byte("payload")
	require.NoError(t, h.Seed(uri("a.txt"), original))
	original[0] = 'X'

	store := remoteio.NewStore(h)
	data, err := remoteio.ReadAll(ctx, store, uri("a.txt"))
	require.NoError(t, err)
	assert.Equal(t, "payload", string(data), "Seed に渡したスライスの書き換えが波及してはいけない")

	// 読み出した側の書き換えも保存内容へ波及しません。
	data[0] = 'Y'
	again, err := remoteio.ReadAll(ctx, store, uri("a.txt"))
	require.NoError(t, err)
	assert.Equal(t, "payload", string(again))
}

// 正常系では通らない経路（書き込み失敗時の後始末など）を試すためのフックです。
func TestWithFailure(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("ストレージ障害")

	h := memio.New(memio.WithFailure(func(op, uri string) error {
		if op == "write" && strings.HasSuffix(uri, "broken.txt") {
			return boom
		}
		return nil
	}))
	store := remoteio.NewStore(h)

	err := remoteio.WriteAll(ctx, store, uri("broken.txt"), []byte("x"))
	assert.ErrorIs(t, err, boom)
	assert.Equal(t, 0, h.Len(), "失敗した書き込みは保存されない")

	require.NoError(t, remoteio.WriteAll(ctx, store, uri("fine.txt"), []byte("x")))
	assert.Equal(t, 1, h.Len(), "対象外の操作は通常どおり処理される")
}

func TestWithClockAndScheme(t *testing.T) {
	ctx := context.Background()
	fixed := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)

	// gs を名乗らせると、利用側のテストが本番と同じ URI を書けます。
	h := memio.New(memio.WithScheme(remoteio.SchemeGCS), memio.WithClock(func() time.Time { return fixed }))
	store := remoteio.NewStore(h)

	gcsURI := remoteio.BuildURI(remoteio.SchemeGCS, "bucket", "a.txt")
	require.NoError(t, remoteio.WriteAll(ctx, store, gcsURI, []byte("x")))

	info, err := store.Stat(ctx, gcsURI)
	require.NoError(t, err)
	assert.Equal(t, fixed, info.ModTime)
	assert.Equal(t, remoteio.SchemeGCS, h.Scheme())
}

// Copier を実装しているので、同一スキーム内の Copy はストリーム中継になりません。
func TestCopyTo(t *testing.T) {
	ctx := context.Background()
	h := memio.New()
	require.NoError(t, h.Seed(uri("src.txt"), []byte("payload")))

	store := remoteio.NewStore(h)
	require.NoError(t, store.Copy(ctx, uri("src.txt"), uri("dst.txt")))

	data, err := remoteio.ReadAll(ctx, store, uri("dst.txt"))
	require.NoError(t, err)
	assert.Equal(t, "payload", string(data))

	err = store.Copy(ctx, uri("missing.txt"), uri("never.txt"))
	assert.ErrorIs(t, err, remoteio.ErrNotExist)
}
