package remoteio

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScheme(t *testing.T) {
	tests := []struct {
		name string
		uri  string
		want string
	}{
		{"GCS", "gs://bucket/key", SchemeGCS},
		{"S3", "s3://bucket/key", SchemeS3},
		{"file", "file:///tmp/a.txt", SchemeFile},
		{"ローカル相対パス", "data/a.txt", ""},
		{"ローカル絶対パス", "/tmp/a.txt", ""},
		{"空文字", "", ""},
		{"区切りだけ", "://bucket", ""},
		{"区切りを持たないURI", "mailto:a@example.com", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Scheme(tt.uri))
		})
	}
}

// 素の前方一致だと "gsfoo://" のような別スキームまで拾ってしまいます。
// 判定を HasScheme に集約している理由がこれです。
func TestHasSchemeRejectsPrefixCollision(t *testing.T) {
	assert.True(t, HasScheme("gs://bucket/key", SchemeGCS))
	assert.False(t, HasScheme("gsfoo://bucket/key", SchemeGCS))
	assert.False(t, HasScheme("s3://bucket/key", SchemeGCS))
}

func TestParseURI(t *testing.T) {
	t.Run("バケットとキーに分解する", func(t *testing.T) {
		scheme, bucket, object, err := ParseURI("gs://my-bucket/a/b.txt")
		require.NoError(t, err)
		assert.Equal(t, SchemeGCS, scheme)
		assert.Equal(t, "my-bucket", bucket)
		assert.Equal(t, "a/b.txt", object)
	})

	t.Run("キーが空でも通る", func(t *testing.T) {
		_, bucket, object, err := ParseURI("s3://my-bucket")
		require.NoError(t, err)
		assert.Equal(t, "my-bucket", bucket)
		assert.Empty(t, object)
	})

	t.Run("スキームが無ければ ErrInvalidURI", func(t *testing.T) {
		_, _, _, err := ParseURI("data/a.txt")
		assert.ErrorIs(t, err, ErrInvalidURI)
	})

	t.Run("バケット名が空なら ErrInvalidURI", func(t *testing.T) {
		_, _, _, err := ParseURI("gs:///a.txt")
		assert.ErrorIs(t, err, ErrInvalidURI)
	})
}

func TestParseObjectURIRequiresObject(t *testing.T) {
	// gs://bucket のような URI を通すと、バケット操作と取り違えたときに
	// 「不在」なのか「URI が不正」なのか区別できなくなります。
	_, _, err := ParseObjectURI(SchemeGCS, "gs://bucket")
	assert.ErrorIs(t, err, ErrInvalidURI)

	_, _, err = ParseObjectURI(SchemeGCS, "s3://bucket/key")
	assert.ErrorIs(t, err, ErrInvalidURI, "担当外のスキームは拒否する")

	bucket, object, err := ParseObjectURI(SchemeGCS, "gs://bucket/key")
	require.NoError(t, err)
	assert.Equal(t, "bucket", bucket)
	assert.Equal(t, "key", object)
}

func TestBuildURIRoundTrip(t *testing.T) {
	tests := []struct {
		name           string
		bucket, object string
		want           string
	}{
		{"通常", "b", "a/x.txt", "gs://b/a/x.txt"},
		{"キーなし", "b", "", "gs://b"},
		{"先頭スラッシュは畳む", "b", "/a/x.txt", "gs://b/a/x.txt"},
		{"空白を含むキーはそのまま", "b", "a b.txt", "gs://b/a b.txt"},
		{"? を含むキーはそのまま", "b", "a?b.txt", "gs://b/a?b.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildURI(SchemeGCS, tt.bucket, tt.object)
			assert.Equal(t, tt.want, got)

			if tt.object != "" {
				// URL としてエンコードしないことが、キーを取り違えないための条件です。
				_, bucket, object, err := ParseURI(got)
				require.NoError(t, err)
				assert.Equal(t, tt.bucket, bucket)
				assert.Equal(t, filepath.ToSlash(tt.object[len(tt.object)-len(object):]), object)
			}
		})
	}
}

// v1 は区切り文字を指定したときだけ正規化していたため、"data" が "data-archive/" にも
// 一致していました。v2 は常に正規化します。
func TestListPrefixAlwaysNormalizes(t *testing.T) {
	tests := []struct {
		name string
		uri  string
		want string
	}{
		{"末尾に区切りを足す", "gs://b/data", "gs://b/data/"},
		{"既に区切りで終わる", "gs://b/data/", "gs://b/data/"},
		{"バケット直下はそのまま", "gs://b", "gs://b"},
		{"ローカルパス", "out/dir", "out/dir/"},
		{"空文字", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ListPrefix(tt.uri))
			assert.Equal(t, tt.want, ListPrefix(ListPrefix(tt.uri)), "冪等であること")
		})
	}
}

func TestDir(t *testing.T) {
	t.Run("リモート", func(t *testing.T) {
		assert.Equal(t, "gs://b/a/", Dir("gs://b/a/x.txt"))
		assert.Equal(t, "gs://b/a/", Dir("gs://b/a/"))
		assert.Equal(t, "gs://b/", Dir("gs://b/x.txt"))
		assert.Equal(t, "gs://b/", Dir("gs://b"))
	})

	t.Run("空白や ? を含むキーを壊さない", func(t *testing.T) {
		// net/url を通すとここが %20 やクエリ区切りに化けます。
		assert.Equal(t, "gs://b/a b/", Dir("gs://b/a b/x y.txt"))
		assert.Equal(t, "gs://b/a?b/", Dir("gs://b/a?b/x.txt"))
	})

	t.Run("ローカル", func(t *testing.T) {
		sep := string(filepath.Separator)
		assert.Equal(t, "."+sep, Dir("x.txt"))
		assert.Equal(t, filepath.Join("a", "b")+sep, Dir(filepath.Join("a", "b", "x.txt")))
	})

	t.Run("空文字", func(t *testing.T) {
		assert.Empty(t, Dir(""))
	})
}

func TestJoin(t *testing.T) {
	assert.Equal(t, "gs://b/a/x.txt", Join("gs://b/a", "x.txt"))
	assert.Equal(t, "gs://b/a/x.txt", Join("gs://b/a/", "/x.txt"))
	assert.Equal(t, "gs://b/a", Join("gs://b/a", ""))
	assert.Equal(t, filepath.Join("a", "x.txt"), Join("a", "x.txt"))
	assert.Equal(t, "x.txt", Join("", "x.txt"))
}

func TestIndexedPath(t *testing.T) {
	got, err := IndexedPath("path/to/image.png", 1)
	require.NoError(t, err)
	assert.Equal(t, "path/to/image_1.png", got)

	got, err = IndexedPath("gs://b/dir/name", 2)
	require.NoError(t, err)
	assert.Equal(t, "gs://b/dir/name_2", got)

	_, err = IndexedPath("a.png", 0)
	assert.ErrorIs(t, err, ErrInvalidURI)
}

func TestNormalizeBucketName(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"  my-bucket  ", "my-bucket"},
		{"gs://my-bucket/", "my-bucket"},
		{"s3://my-bucket", "my-bucket"},
		{"file://my-bucket", "my-bucket"},
		{"/my-bucket/", "my-bucket"},
		{"my-bucket", "my-bucket"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, NormalizeBucketName(tt.in))
		})
	}
}
