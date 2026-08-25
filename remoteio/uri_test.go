package remoteio

import (
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseRemoteURI は GCS と S3 両方のパースロジックを検証します
func TestParseRemoteURI(t *testing.T) {
	tests := []struct {
		name       string
		uri        string
		wantBucket string
		wantPath   string
		wantErr    bool
	}{
		// --- GCS Cases ---
		{
			name:       "GCS: valid full URI",
			uri:        "gs://my-bucket/path/to/file.txt",
			wantBucket: "my-bucket",
			wantPath:   "path/to/file.txt",
			wantErr:    false,
		},
		{
			name:       "GCS: valid bucket only",
			uri:        "gs://my-bucket",
			wantBucket: "my-bucket",
			wantPath:   "",
			wantErr:    false,
		},
		{
			name:       "GCS: valid bucket with trailing slash",
			uri:        "gs://my-bucket/",
			wantBucket: "my-bucket",
			wantPath:   "",
			wantErr:    false,
		},
		{
			name:    "GCS: empty bucket name",
			uri:     "gs:///path/to/obj",
			wantErr: true,
		},
		{
			name:    "GCS: only scheme",
			uri:     "gs://",
			wantErr: true,
		},

		// --- S3 Cases ---
		{
			name:       "S3: valid full URI",
			uri:        "s3://my-s3-bucket/images/photo.png",
			wantBucket: "my-s3-bucket",
			wantPath:   "images/photo.png",
			wantErr:    false,
		},
		{
			name:       "S3: valid bucket with trailing slash",
			uri:        "s3://my-s3-bucket/",
			wantBucket: "my-s3-bucket",
			wantPath:   "",
			wantErr:    false,
		},
		{
			name:    "S3: only scheme",
			uri:     "s3://",
			wantErr: true,
		},

		// --- Error Cases ---
		{
			name:    "Invalid scheme: http",
			uri:     "http://example.com",
			wantErr: true,
		},
		{
			name:    "Invalid scheme: local path",
			uri:     "/local/path/to/file",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotBucket, gotPath, err := ParseRemoteURI(tt.uri)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantBucket, gotBucket)
				assert.Equal(t, tt.wantPath, gotPath)
			}
		})
	}
}

// TestBuildURI は URI 作成ロジックを検証します
func TestBuildURI(t *testing.T) {
	t.Run("BuildGCSURI", func(t *testing.T) {
		assert.Equal(t, "gs://my-bucket/path/to/obj", BuildGCSURI("my-bucket", "path/to/obj"))
		assert.Equal(t, "gs://my-bucket/path/to/obj", BuildGCSURI("my-bucket", "/path/to/obj")) // スラッシュ重複除去の確認
		assert.Equal(t, "gs://my-bucket", BuildGCSURI("my-bucket", ""))
	})

	t.Run("BuildS3URI", func(t *testing.T) {
		assert.Equal(t, "s3://my-s3-bucket/images/photo.png", BuildS3URI("my-s3-bucket", "images/photo.png"))
		assert.Equal(t, "s3://my-s3-bucket", BuildS3URI("my-s3-bucket", ""))
	})
}

// TestIsRemoteURI は リモート判定を検証します
func TestIsRemoteURI(t *testing.T) {
	assert.True(t, IsRemoteURI("gs://bucket/obj"))
	assert.True(t, IsRemoteURI("s3://bucket/obj"))
	assert.False(t, IsRemoteURI("/local/path"))
	assert.False(t, IsRemoteURI("http://web.com"))
	assert.False(t, IsRemoteURI("gs://")) // バケット名がない場合はパースエラー＝falseになる
}

// TestSchemePrefix は、スキームの取り出しが Router.resolve と同じ解釈になることを確かめます。
// 呼び出し側が独自に判定を書くと、ここでずれます。
func TestSchemePrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "GCS", path: "gs://bucket/obj", want: "gs://"},
		{name: "S3", path: "s3://bucket/obj", want: "s3://"},
		{name: "未対応スキームもそのまま取り出す", path: "ftp://host/file", want: "ftp://"},
		{name: "絶対パスは空", path: "/var/tmp/file.txt", want: ""},
		{name: "相対パスは空", path: "data/file.txt", want: ""},
		{name: "空文字は空", path: "", want: ""},
		{name: "先頭が区切りなら空（スキーム名が無い）", path: "://bucket/obj", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, SchemePrefix(tt.path))
		})
	}
}

func TestNormalizeBucketName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "裸の名前はそのまま", input: "my-bucket", want: "my-bucket"},
		{name: "前後の空白を落とす", input: "  my-bucket  ", want: "my-bucket"},
		{
			// コンソールから貼るとこの形になります。素通しすると BuildGCSURI が
			// gs://gs://my-bucket//path を作ります。
			name:  "gs:// プレフィックスを落とす",
			input: "gs://my-bucket",
			want:  "my-bucket",
		},
		{name: "s3:// プレフィックスを落とす", input: "s3://my-bucket", want: "my-bucket"},
		{name: "末尾スラッシュを落とす", input: "gs://my-bucket/", want: "my-bucket"},
		{name: "空白とスキームと両端スラッシュの複合", input: " /gs://my-bucket/ ", want: "gs://my-bucket"},
		{name: "空文字列は空文字列", input: "", want: ""},
		{
			// バケットとパスの分解は担当しません（ParseRemoteURI の役目）。
			name:  "オブジェクトパスは分解しない",
			input: "gs://my-bucket/a/b.txt",
			want:  "my-bucket/a/b.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeBucketName(tt.input); got != tt.want {
				t.Errorf("NormalizeBucketName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// 正規化を通した値が BuildGCSURI で二重スキームにならないこと。
// この 2 つは対で使われるため、片方だけ変えると壊れます。
func TestNormalizeBucketNameFeedsBuildGCSURI(t *testing.T) {
	got := BuildGCSURI(NormalizeBucketName("gs://my-bucket/"), "reviews/1.json")
	if want := "gs://my-bucket/reviews/1.json"; got != want {
		t.Errorf("BuildGCSURI = %q, want %q", got, want)
	}
}

// スキームを固定しない分解。第三者が新しいスキームのハンドラを書けるように公開しています。
func TestParseBucketURI(t *testing.T) {
	tests := []struct {
		name       string
		uri        string
		wantScheme string
		wantBucket string
		wantPath   string
		wantErr    bool
	}{
		{name: "GCS", uri: "gs://b/a/c.txt", wantScheme: "gs://", wantBucket: "b", wantPath: "a/c.txt"},
		{name: "S3", uri: "s3://b/a/c.txt", wantScheme: "s3://", wantBucket: "b", wantPath: "a/c.txt"},
		{
			// ParseRemoteURI と違い、未知のスキームも分解できます。
			name: "未知のスキームでも分解する", uri: "azure://b/a.txt",
			wantScheme: "azure://", wantBucket: "b", wantPath: "a.txt",
		},
		{name: "バケットのみ", uri: "gs://b", wantScheme: "gs://", wantBucket: "b", wantPath: ""},
		{name: "末尾スラッシュ", uri: "gs://b/", wantScheme: "gs://", wantBucket: "b", wantPath: ""},
		{name: "キーに空白や ? があってもそのまま", uri: "gs://b/my dir/a?b.txt", wantScheme: "gs://", wantBucket: "b", wantPath: "my dir/a?b.txt"},
		{name: "スキームなしはエラー", uri: "/local/path", wantErr: true},
		{name: "バケット名が空はエラー", uri: "gs:///a.txt", wantErr: true},
		{name: "スキームだけはエラー", uri: "gs://", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme, bucket, path, err := ParseBucketURI(tt.uri)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantScheme, scheme)
			assert.Equal(t, tt.wantBucket, bucket)
			assert.Equal(t, tt.wantPath, path)
		})
	}
}

// ハンドラが担当外のスキームを受け取ったまま処理してしまうのを防ぐための検証。
func TestParseSchemeURI(t *testing.T) {
	t.Run("スキームが一致すれば分解する", func(t *testing.T) {
		bucket, path, err := ParseSchemeURI(PrefixGCS, "gs://b/a.txt")
		require.NoError(t, err)
		assert.Equal(t, "b", bucket)
		assert.Equal(t, "a.txt", path)
	})

	// これが無いと、gcs.Handler に s3:// を直接渡したとき GCS のバケットを触ります。
	t.Run("スキームが違えばエラー", func(t *testing.T) {
		_, _, err := ParseSchemeURI(PrefixGCS, "s3://b/a.txt")
		assert.ErrorContains(t, err, "スキームが一致しません")
	})

	t.Run("プレフィックスは空でもよい", func(t *testing.T) {
		_, path, err := ParseSchemeURI(PrefixGCS, "gs://b")
		require.NoError(t, err)
		assert.Empty(t, path)
	})
}

func TestParseSchemeObjectURI(t *testing.T) {
	t.Run("オブジェクト名があれば分解する", func(t *testing.T) {
		bucket, object, err := ParseSchemeObjectURI(PrefixS3, "s3://b/a/c.txt")
		require.NoError(t, err)
		assert.Equal(t, "b", bucket)
		assert.Equal(t, "a/c.txt", object)
	})

	// 不在なのか URI が不正なのか区別できなくなるため、バケットだけの URI は拒否します。
	t.Run("オブジェクト名が空ならエラー", func(t *testing.T) {
		_, _, err := ParseSchemeObjectURI(PrefixS3, "s3://b")
		assert.ErrorContains(t, err, "オブジェクト名が空です")
	})
}

func TestBuildURIWithArbitraryScheme(t *testing.T) {
	assert.Equal(t, "azure://b/a/c.txt", BuildURI("azure://", "b", "a/c.txt"))
	assert.Equal(t, "azure://b", BuildURI("azure://", "b", ""))
	assert.Equal(t, "azure://b/a.txt", BuildURI("azure://", "b", "/a.txt"))
}

// TestSchemeConstants は、公開しているスキームプレフィックスの綴りを固定します。
//
// プレフィックスは非公開の名前定数から導出しているため、名前を書き換えると
// プレフィックスが黙って変わります。gs:// / s3:// / file:// は URI として外部に
// 出る値なので、実際の綴りをここで押さえます。
func TestSchemeConstants(t *testing.T) {
	tests := []struct {
		name       string
		prefix     string
		wantScheme string
	}{
		{name: "GCS", prefix: PrefixGCS, wantScheme: "gs"},
		{name: "S3", prefix: PrefixS3, wantScheme: "s3"},
		{name: "file", prefix: PrefixFile, wantScheme: "file"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantScheme+"://", tt.prefix, "プレフィックス")

			// SchemePrefix はプレフィックス側と同じ形を返すこと（Router の登録キー）。
			assert.Equal(t, tt.prefix, SchemePrefix(tt.prefix+"bucket/obj.txt"))
		})
	}
}

// TestSchemeConstantsMatchNetURL は、プレフィックスから区切りを落とした形が
// net/url の url.URL.Scheme と一致することを確認します。名前の形が必要な呼び出し側は
// strings.TrimSuffix(PrefixGCS, "://") で得るので、そこがずれると使えなくなります。
func TestSchemeConstantsMatchNetURL(t *testing.T) {
	for _, prefix := range []string{PrefixGCS, PrefixS3, PrefixFile} {
		t.Run(prefix, func(t *testing.T) {
			u, err := url.Parse(prefix + "bucket/obj.txt")
			require.NoError(t, err)
			assert.Equal(t, strings.TrimSuffix(prefix, "://"), u.Scheme)
		})
	}
}
