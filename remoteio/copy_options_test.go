package remoteio_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-remote-io/remoteio/memio"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCopyHonoursWriteOptions は、WriteOption が同一スキームのコピーでも効くことを
// 検証します。CopyTo はオプションを受け取らないため、サーバーサイドコピーへ落とすと
// 指定が黙って消えます（WithIfNotExists を付けたのに上書きされる、など）。
func TestCopyHonoursWriteOptions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := remoteio.NewStore(memio.New(memio.WithScheme("gs")))
	require.NoError(t, remoteio.WriteAll(ctx, store, "gs://b/src.txt", []byte("new")))
	require.NoError(t, remoteio.WriteAll(ctx, store, "gs://b/dst.txt", []byte("existing")))

	// 同一スキームでも WithIfNotExists は効く（既存を上書きしない）。
	err := store.Copy(ctx, "gs://b/src.txt", "gs://b/dst.txt", remoteio.WithIfNotExists())
	require.ErrorIs(t, err, remoteio.ErrExist)

	got, err := remoteio.ReadAll(ctx, store, "gs://b/dst.txt")
	require.NoError(t, err)
	assert.Equal(t, "existing", string(got), "拒否したはずのコピーが上書きしています")

	// 指定した Content-Type も効く。
	require.NoError(t, store.Copy(ctx, "gs://b/src.txt", "gs://b/typed.txt",
		remoteio.WithContentType("application/json")))
	info, err := store.Stat(ctx, "gs://b/typed.txt")
	require.NoError(t, err)
	assert.Equal(t, "application/json", info.ContentType)
}

// TestCopyCarriesSourceAttributes は、指定しなかった Content-Type とメタデータを
// コピー元から引き継ぐことを検証します。引き継がないと、ストリーム中継の経路だけが
// 既定の Content-Type で塗り潰し、同じ Copy がスキームの組み合わせで別の結果になります。
func TestCopyCarriesSourceAttributes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := remoteio.NewStore(
		memio.New(memio.WithScheme("gs")),
		memio.New(memio.WithScheme("s3")),
	)
	require.NoError(t, remoteio.WriteAll(ctx, store, "gs://b/src.png", []byte("img"),
		remoteio.WithContentType("image/png"),
		remoteio.WithMetadata(map[string]string{"origin": "job-1"}),
	))

	for _, dst := range []string{"gs://b/same.png", "s3://b/cross.png"} {
		require.NoError(t, store.Copy(ctx, "gs://b/src.png", dst))

		info, err := store.Stat(ctx, dst)
		require.NoError(t, err, dst)
		assert.Equal(t, "image/png", info.ContentType, dst)
		assert.Equal(t, "job-1", info.Metadata["origin"], dst)
	}

	// 明示した Content-Type は引き継ぎに勝つ。
	require.NoError(t, store.Copy(ctx, "gs://b/src.png", "s3://b/typed.bin",
		remoteio.WithContentType("application/octet-stream")))
	info, err := store.Stat(ctx, "s3://b/typed.bin")
	require.NoError(t, err)
	assert.Equal(t, "application/octet-stream", info.ContentType)
}

// TestSubRejectsRelativeSegments は、スコープ付きストアが ".." を含む名前を拒むことを
// 検証します。ローカルの Join は filepath.Join なので、通すとスコープの外を読み書きできます
// （ErrAbsoluteName が防いでいるのと同じ「別の場所へ書ける」）。
func TestSubRejectsRelativeSegments(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := remoteio.NewStore(memio.New(memio.WithScheme("gs")))
	dir := t.TempDir()
	require.NoError(t, remoteio.WriteAll(ctx, store, filepath.Join(dir, "secret.txt"), []byte("secret")))

	local := store.Sub(filepath.Join(dir, "jobs"))
	remote := store.Sub("gs://b/jobs")

	for _, name := range []string{"../secret.txt", "a/../../secret.txt", "./a.txt", ".."} {
		_, err := local.Open(ctx, name)
		assert.ErrorIs(t, err, remoteio.ErrInvalidURI, "local Open(%q)", name)

		err = remoteio.WriteAll(ctx, local, name, []byte("x"))
		assert.ErrorIs(t, err, remoteio.ErrInvalidURI, "local WriteAll(%q)", name)

		err = remoteio.WriteAll(ctx, remote, name, []byte("x"))
		assert.ErrorIs(t, err, remoteio.ErrInvalidURI, "remote WriteAll(%q)", name)
	}

	// スコープ外へ書けていないこと。
	got, err := remoteio.ReadAll(ctx, store, filepath.Join(dir, "secret.txt"))
	require.NoError(t, err)
	assert.Equal(t, "secret", string(got))

	// 普通の名前は通る（"..", "." を含まないドット始まりも含む）。
	require.NoError(t, remoteio.WriteAll(ctx, local, "a/b.txt", []byte("ok")))
	require.NoError(t, remoteio.WriteAll(ctx, local, ".hidden", []byte("ok")))
}
