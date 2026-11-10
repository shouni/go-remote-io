package remoteio

import (
	"maps"
	"mime"
)

// DefaultContentType is the default used when no Content-Type is given.
const DefaultContentType = "text/plain; charset=utf-8"

// ListOptions is the result of resolving ListOption values, and is what Handler.List receives.
//
// The resolver is not exported because the only thing to implement is Handler.
// Implementations receive only resolved values.
type ListOptions struct {
	// Delimiter is the hierarchy delimiter.
	// When empty, everything under the prefix is listed recursively.
	// When set, only direct children are listed, and folded hierarchies appear as Entry values with IsPrefix.
	Delimiter string
}

// ListOption is a functional option that changes how List behaves.
type ListOption func(*ListOptions)

// WithDelimiter sets the hierarchy delimiter and limits the listing to the prefix's direct children.
//
// With it, "pseudo-directories" are listed in addition to the direct child objects.
// Pseudo-directories arrive as Entry values with IsPrefix true, so the caller does
// not need to look at the trailing character.
//
// This is what a list of job IDs needs. Without a delimiter every object below is
// returned, so with 3 artifacts per job the caller receives 3 times the data and
// deduplicates it itself. The delimiter moves that walk to the server.
func WithDelimiter(delimiter string) ListOption {
	return func(o *ListOptions) { o.Delimiter = delimiter }
}

// newListOptions folds the options into settings.
func newListOptions(opts ...ListOption) ListOptions {
	var o ListOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	return o
}

// WriteOptions is the result of resolving WriteOption values, and is what Handler.Write receives.
type WriteOptions struct {
	// ContentType is the MIME type of the object to write. Empty means DefaultContentType.
	ContentType string
	// CacheControl is the Cache-Control header. Empty means it is not set.
	CacheControl string
	// ContentDisposition is the Content-Disposition header. Empty means it is not set.
	ContentDisposition string
	// Metadata is arbitrary user-defined metadata. Empty means it is not set.
	// On the local file system it is not stored and is ignored.
	Metadata map[string]string
	// IfNotExists says to write only when the target does not exist.
	// If it already exists, Write returns an error containing ErrExist.
	IfNotExists bool
}

// WriteOption is a functional option that changes how Write behaves.
type WriteOption func(*WriteOptions)

// newWriteOptions folds the options into settings.
// An empty Content-Type is filled in with DefaultContentType.
func newWriteOptions(opts ...WriteOption) WriteOptions {
	o := WriteOptions{ContentType: DefaultContentType}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	// Handle the fallback for an explicitly passed empty string in one place.
	if o.ContentType == "" {
		o.ContentType = DefaultContentType
	}
	return o
}

// WithContentType sets the MIME type.
func WithContentType(contentType string) WriteOption {
	return func(o *WriteOptions) { o.ContentType = contentType }
}

// WithCacheControl sets the Cache-Control header.
func WithCacheControl(cacheControl string) WriteOption {
	return func(o *WriteOptions) { o.CacheControl = cacheControl }
}

// WithMetadata sets user-defined metadata.
// Passing it several times accumulates (the last value wins for the same key).
// Local writes silently ignore it, like Content-Type and the rest.
func WithMetadata(metadata map[string]string) WriteOption {
	return func(o *WriteOptions) {
		if len(metadata) == 0 {
			return
		}
		if o.Metadata == nil {
			o.Metadata = make(map[string]string, len(metadata))
		}
		maps.Copy(o.Metadata, metadata)
	}
}

// WithInline asks for inline playback in the browser.
func WithInline() WriteOption {
	return func(o *WriteOptions) { o.ContentDisposition = "inline" }
}

// WithAttachment forces a download and sets the file name.
func WithAttachment(filename string) WriteOption {
	return func(o *WriteOptions) {
		if filename == "" {
			o.ContentDisposition = "attachment"
			return
		}
		o.ContentDisposition = mime.FormatMediaType("attachment", map[string]string{"filename": filename})
	}
}

// WithIfNotExists says to write only when the target does not exist.
//
// Checking with Exists before Write races, because another process can write
// between those two calls. GCS preconditions and S3's If-None-Match are used as is,
// moving the decision to the storage side.
func WithIfNotExists() WriteOption {
	return func(o *WriteOptions) { o.IfNotExists = true }
}
