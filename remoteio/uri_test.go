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
		{"local relative path", "data/a.txt", ""},
		{"local absolute path", "/tmp/a.txt", ""},
		{"empty", "", ""},
		{"separator only", "://bucket", ""},
		{"URI without separator", "mailto:a@example.com", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Scheme(tt.uri))
		})
	}
}

// A bare prefix match would also catch another scheme such as "gsfoo://".
// That is why the check is gathered in HasScheme.
func TestHasSchemeRejectsPrefixCollision(t *testing.T) {
	assert.True(t, HasScheme("gs://bucket/key", SchemeGCS))
	assert.False(t, HasScheme("gsfoo://bucket/key", SchemeGCS))
	assert.False(t, HasScheme("s3://bucket/key", SchemeGCS))
}

func TestParseURI(t *testing.T) {
	t.Run("splits into bucket and key", func(t *testing.T) {
		scheme, bucket, object, err := ParseURI("gs://my-bucket/a/b.txt")
		require.NoError(t, err)
		assert.Equal(t, SchemeGCS, scheme)
		assert.Equal(t, "my-bucket", bucket)
		assert.Equal(t, "a/b.txt", object)
	})

	t.Run("accepts an empty key", func(t *testing.T) {
		_, bucket, object, err := ParseURI("s3://my-bucket")
		require.NoError(t, err)
		assert.Equal(t, "my-bucket", bucket)
		assert.Empty(t, object)
	})

	t.Run("no scheme returns ErrInvalidURI", func(t *testing.T) {
		_, _, _, err := ParseURI("data/a.txt")
		assert.ErrorIs(t, err, ErrInvalidURI)
	})

	t.Run("empty bucket returns ErrInvalidURI", func(t *testing.T) {
		_, _, _, err := ParseURI("gs:///a.txt")
		assert.ErrorIs(t, err, ErrInvalidURI)
	})
}

func TestParseObjectURIRequiresObject(t *testing.T) {
	// Letting a URI such as gs://bucket through makes it impossible, when it is mistaken
	// for a bucket operation, to tell "not there" from "invalid URI".
	_, _, err := ParseObjectURI(SchemeGCS, "gs://bucket")
	assert.ErrorIs(t, err, ErrInvalidURI)

	_, _, err = ParseObjectURI(SchemeGCS, "s3://bucket/key")
	assert.ErrorIs(t, err, ErrInvalidURI, "a foreign scheme is rejected")

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
		{"plain", "b", "a/x.txt", "gs://b/a/x.txt"},
		{"no key", "b", "", "gs://b"},
		{"collapses leading slash", "b", "/a/x.txt", "gs://b/a/x.txt"},
		{"keeps a key with spaces", "b", "a b.txt", "gs://b/a b.txt"},
		{"keeps a key with ?", "b", "a?b.txt", "gs://b/a?b.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildURI(SchemeGCS, tt.bucket, tt.object)
			assert.Equal(t, tt.want, got)

			if tt.object != "" {
				// Not encoding as a URL is the condition for not mistaking the key.
				_, bucket, object, err := ParseURI(got)
				require.NoError(t, err)
				assert.Equal(t, tt.bucket, bucket)
				assert.Equal(t, filepath.ToSlash(tt.object[len(tt.object)-len(object):]), object)
			}
		})
	}
}

// Normalizing only when a delimiter is given would make "data" also match
// "data-archive/". Always normalizing prevents that.
func TestListPrefixAlwaysNormalizes(t *testing.T) {
	tests := []struct {
		name string
		uri  string
		want string
	}{
		{"appends a trailing separator", "gs://b/data", "gs://b/data/"},
		{"already ends with separator", "gs://b/data/", "gs://b/data/"},
		{"bucket root unchanged", "gs://b", "gs://b"},
		{"local path", "out/dir", "out/dir/"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ListPrefix(tt.uri))
			assert.Equal(t, tt.want, ListPrefix(ListPrefix(tt.uri)), "idempotent")
		})
	}
}

func TestDir(t *testing.T) {
	t.Run("remote", func(t *testing.T) {
		assert.Equal(t, "gs://b/a/", Dir("gs://b/a/x.txt"))
		assert.Equal(t, "gs://b/a/", Dir("gs://b/a/"))
		assert.Equal(t, "gs://b/", Dir("gs://b/x.txt"))
		assert.Equal(t, "gs://b/", Dir("gs://b"))
	})

	t.Run("keeps keys with spaces or ?", func(t *testing.T) {
		// Through net/url this would turn into %20 or a query separator.
		assert.Equal(t, "gs://b/a b/", Dir("gs://b/a b/x y.txt"))
		assert.Equal(t, "gs://b/a?b/", Dir("gs://b/a?b/x.txt"))
	})

	t.Run("local", func(t *testing.T) {
		sep := string(filepath.Separator)
		assert.Equal(t, "."+sep, Dir("x.txt"))
		assert.Equal(t, filepath.Join("a", "b")+sep, Dir(filepath.Join("a", "b", "x.txt")))
	})

	t.Run("empty", func(t *testing.T) {
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
