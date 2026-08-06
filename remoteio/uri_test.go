package remoteio

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
