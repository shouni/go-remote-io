package remoteio

import (
	"context"
	"fmt"
	"io"
	"iter"
	"net/url"
	"path/filepath"
	"strings"
)

// filePrefix is the prefix of the file:// scheme.
// It is derived from the scheme name, so this is the only literal.
const filePrefix = SchemeFile + schemeSeparator

// fileHandler maps file:// URIs to local paths and delegates to localHandler.
//
// Local is treated as the fallback for "paths without a scheme", but values coming
// from configuration files or other tools sometimes carry file://, and as is they
// would be rejected as an unsupported scheme. Keeping the mapping in one place keeps
// the local implementation as the single localHandler.
type fileHandler struct {
	local Handler
}

var _ Handler = fileHandler{}

// NewFileHandler returns a handler for the file:// scheme.
//
// It expects an empty host part, as in file:///tmp/a.txt, and treats what remains
// after dropping file:// as a local path (file://data/a.txt becomes the relative path
// data/a.txt). Percent-encoding is decoded as the URI rules say, so a file whose name
// contains % must be written as %25.
func NewFileHandler() Handler { return fileHandler{local: NewLocalHandler()} }

// Scheme returns "file".
func (fileHandler) Scheme() string { return SchemeFile }

func (h fileHandler) Open(ctx context.Context, uri string) (io.ReadCloser, error) {
	path, err := toLocalPath(uri)
	if err != nil {
		return nil, err
	}
	return h.local.Open(ctx, path)
}

// Stat returns the metadata of a file:// URI. The URI is put back into the form used for the query.
func (h fileHandler) Stat(ctx context.Context, uri string) (ObjectInfo, error) {
	path, err := toLocalPath(uri)
	if err != nil {
		return ObjectInfo{}, err
	}
	info, err := h.local.Stat(ctx, path)
	if err != nil {
		return ObjectInfo{}, err
	}
	info.URI = uri
	return info, nil
}

// List turns the URIs of the results back into the file:// form before returning them.
// This matches the GCS / S3 handlers returning gs:// / s3:// URIs, so that a received
// Entry can be passed as is to the next call.
func (h fileHandler) List(ctx context.Context, uri string, opts ListOptions) iter.Seq2[Entry, error] {
	path, err := toLocalPath(uri)
	if err != nil {
		return errSeq(err)
	}
	return func(yield func(Entry, error) bool) {
		for entry, err := range h.local.List(ctx, path, opts) {
			if err != nil {
				yield(Entry{}, err)
				return
			}
			entry.URI = toFileURI(entry.URI)
			if !yield(entry, nil) {
				return
			}
		}
	}
}

func (h fileHandler) Write(ctx context.Context, uri string, src io.Reader, opts WriteOptions) error {
	path, err := toLocalPath(uri)
	if err != nil {
		return err
	}
	return h.local.Write(ctx, path, src, opts)
}

func (h fileHandler) Delete(ctx context.Context, uri string) error {
	path, err := toLocalPath(uri)
	if err != nil {
		return err
	}
	return h.local.Delete(ctx, path)
}

// toLocalPath converts a file:// URI to a local path.
func toLocalPath(uri string) (string, error) {
	if !strings.HasPrefix(uri, filePrefix) {
		return "", fmt.Errorf("%w: not a file:// URI (%s)", ErrInvalidURI, uri)
	}
	body := strings.TrimPrefix(uri, filePrefix)
	if body == "" {
		return "", fmt.Errorf("%w: no path given (%s)", ErrInvalidURI, uri)
	}
	decoded, err := url.PathUnescape(body)
	if err != nil {
		return "", wrapf(err, "decode file:// URI (%s)", uri)
	}
	return filepath.FromSlash(decoded), nil
}

// toFileURI turns a local path back into a file:// URI.
func toFileURI(path string) string {
	return filePrefix + filepath.ToSlash(path)
}
