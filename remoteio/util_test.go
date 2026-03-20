package remoteio

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestParseGCSURI は GCS URI のパースロジックを検証するのだ
func TestParseGCSURI(t *testing.T) {
	tests := []struct {
		name       string
		uri        string
		wantBucket string
		wantPath   string
		wantErr    bool
	}{
		{
			name:       "valid full URI",
			uri:        "gs://my-bucket/path/to/file.txt",
			wantBucket: "my-bucket",
			wantPath:   "path/to/file.txt",
			wantErr:    false,
		},
		{
			name:       "valid bucket only",
			uri:        "gs://my-bucket",
			wantBucket: "my-bucket",
			wantPath:   "",
			wantErr:    false,
		},
		{
			name:       "valid bucket with trailing slash",
			uri:        "gs://my-bucket/",
			wantBucket: "my-bucket",
			wantPath:   "",
			wantErr:    false,
		},
		{
			name:    "invalid scheme",
			uri:     "http://example.com",
			wantErr: true,
		},
		{
			name:    "empty bucket name",
			uri:     "gs:///path/to/obj",
			wantErr: true,
		},
		{
			name:    "only scheme",
			uri:     "gs://",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotBucket, gotPath, err := ParseGCSURI(tt.uri)
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

// TestParseS3URI は S3 URI のパースロジックを検証するのだ
func TestParseS3URI(t *testing.T) {
	tests := []struct {
		name       string
		uri        string
		wantBucket string
		wantPath   string
		wantErr    bool
	}{
		{
			name:       "valid full S3 URI",
			uri:        "s3://my-s3-bucket/images/photo.png",
			wantBucket: "my-s3-bucket",
			wantPath:   "images/photo.png",
			wantErr:    false,
		},
		{
			name:       "valid S3 bucket with trailing slash",
			uri:        "s3://my-s3-bucket/",
			wantBucket: "my-s3-bucket",
			wantPath:   "",
			wantErr:    false,
		},
		{
			name:    "invalid S3 scheme",
			uri:     "gs://this-is-gcs",
			wantErr: true,
		},
		{
			name:    "empty S3 bucket name",
			uri:     "s3://",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotBucket, gotPath, err := ParseS3URI(tt.uri)
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

// TestIsRemoteURI は リモート判定の統合テストなのだ
func TestIsRemoteURI_Combined(t *testing.T) {
	assert.True(t, IsRemoteURI("gs://bucket/obj"))
	assert.True(t, IsRemoteURI("s3://bucket/obj"))
	assert.False(t, IsRemoteURI("/local/path"))
	assert.False(t, IsRemoteURI("http://web.com"))
}
