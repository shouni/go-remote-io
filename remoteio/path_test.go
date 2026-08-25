package remoteio

import (
	"path/filepath"
	"testing"
)

func TestResolveBaseDir(t *testing.T) {
	sep := string(filepath.Separator)
	tests := []struct {
		name    string
		rawPath string
		want    string
	}{
		{"Remote GCS file", "gs://my-bucket/folder/image.png", "gs://my-bucket/folder/"},
		{"Remote S3 dir already has slash", "s3://my-bucket/folder/", "s3://my-bucket/folder/"},
		{"Remote bucket only", "gs://my-bucket", "gs://my-bucket/"},
		// オブジェクト名は URL ではないため、エンコードしてはいけません。
		// %20 に化けた URI は ParseRemoteURI がデコードしないので、エラーにならずに
		// 別のキーを指してしまいます。
		{"Remote key with space", "gs://my-bucket/my dir/image.png", "gs://my-bucket/my dir/"},
		{"Remote key with question mark", "gs://my-bucket/a?b/c.txt", "gs://my-bucket/a?b/"},
		{"Remote key with multibyte", "s3://my-bucket/日本 語/a.txt", "s3://my-bucket/日本 語/"},
		{"Local file absolute", "/tmp/data/output.json", "/tmp/data" + sep},
		{"Local file relative", "results/test.log", "results" + sep},
		{"Current directory", "main.go", "." + sep},
		{"Empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveBaseDir(tt.rawPath)
			if got != tt.want {
				t.Errorf("ResolveBaseDir(%q) = %q, want %q", tt.rawPath, got, tt.want)
			}
		})
	}
}

func TestResolvePath(t *testing.T) {
	tests := []struct {
		name     string
		baseDir  string
		fileName string
		want     string
		wantErr  bool
	}{
		{"Remote GCS", "gs://bucket/dir", "image.png", "gs://bucket/dir/image.png", false},
		{"Remote S3 with trailing slash", "s3://bucket/dir/", "image.png", "s3://bucket/dir/image.png", false},
		// ResolveBaseDir と同じ理由でエンコードしません。
		{"Remote fileName with space", "gs://bucket/dir", "my file.png", "gs://bucket/dir/my file.png", false},
		{"Remote fileName with plus", "gs://bucket/dir", "a+b.png", "gs://bucket/dir/a+b.png", false},
		{"Remote empty fileName", "gs://bucket/dir", "", "gs://bucket/dir", false},
		{"Local path join", "/tmp/dir", "image.png", filepath.Join("/tmp/dir", "image.png"), false},
		{"Empty fileName", "/tmp/dir", "", filepath.Join("/tmp/dir", ""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolvePath(tt.baseDir, tt.fileName)
			if (err != nil) != tt.wantErr {
				t.Errorf("ResolvePath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ResolvePath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGenerateIndexedPath(t *testing.T) {
	tests := []struct {
		name     string
		basePath string
		index    int
		want     string
		wantErr  bool
	}{
		{"Normal file", "image.png", 1, "image_1.png", false},
		{"File with path", "/tmp/image.png", 5, "/tmp/image_5.png", false},
		{"Remote URI", "gs://bucket/art.jpg", 10, "gs://bucket/art_10.jpg", false},
		{"Dot in directory only", "gs://bucket/dir.v2/name", 1, "gs://bucket/dir.v2/name_1", false},
		{"No extension", "README", 2, "README_2", false},
		{"Zero index error", "image.png", 0, "", true},
		{"Negative index error", "image.png", -1, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateIndexedPath(tt.basePath, tt.index)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateIndexedPath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("GenerateIndexedPath() = %q, want %q", got, tt.want)
			}
		})
	}
}
