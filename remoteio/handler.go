package remoteio

import (
	"context"
	"io"
	"iter"
	"time"
)

// Handler is the read/write implementation for one URI scheme.
// Extending this library to a new storage backend means implementing this one interface.
//
// The extension point is narrowed to this one because having two extension points
// would mean exporting settings types both for the side that takes variadic options
// and for the side that takes resolved settings. Option resolution happens entirely
// inside the library, and implementations receive only the resolved values.
//
// Exists is not a method because it derives from Stat and errors.Is(err, ErrNotExist).
// That removes one method to implement, and the room to get the "absence returns
// (false, nil)" promise wrong. Consumers should call Store.Exists.
type Handler interface {
	// Scheme returns the scheme name this handler is responsible for ("gs" and so on).
	// It does not include the separator ("://").
	//
	// A handler returning the empty string is treated as the fallback (the local
	// file system) that receives paths matching no scheme.
	Scheme() string

	// Open returns a read stream.
	// When the target does not exist, the error must contain ErrNotExist.
	Open(ctx context.Context, uri string) (io.ReadCloser, error)

	// Stat returns metadata.
	// When the target does not exist, the error must contain ErrNotExist.
	Stat(ctx context.Context, uri string) (ObjectInfo, error)

	// List enumerates what is under uri. uri arrives already normalized by ListPrefix.
	//
	// When iteration fails part-way, yield the error once with a zero-value Entry and
	// stop. When the caller breaks, stop iterating as is.
	List(ctx context.Context, uri string, opts ListOptions) iter.Seq2[Entry, error]

	// Write writes the contents of src to uri.
	//
	// Implementations must guarantee that the destination does not change unless the
	// write succeeds. A truncated object must never be left behind by a failure part-way.
	// Local meets this with a temporary file + rename, GCS by aborting via ctx
	// cancellation, and S3 by aborting the multipart upload.
	//
	// When opts.IfNotExists is true, it returns an error containing ErrExist if the target already exists.
	Write(ctx context.Context, uri string, src io.Reader, opts WriteOptions) error

	// Delete removes the target. Absence is not an error (idempotent).
	Delete(ctx context.Context, uri string) error
}

// Copier is an optional interface implemented by handlers that can copy
// server-side within the same scheme.
//
// Store.Copy falls through to it only when the source and destination resolve to
// the same handler and that handler implements Copier. Otherwise it relays a stream
// as before, so the caller needs no branching.
//
// Without it, even a copy within one bucket sends every byte through the client.
// A detour actually happened: a consumer held a second client just to call
// CopierFrom itself.
type Copier interface {
	CopyTo(ctx context.Context, src, dst string) error
}

// Signer is an optional interface implemented by handlers that can generate signed URLs.
//
// As an independent interface, whatever bundles several schemes would have to hold a
// separate signer-only dispatch. As an optional handler feature, dispatch happens in
// one place only, Router.
type Signer interface {
	SignURL(ctx context.Context, uri, method string, expires time.Duration) (string, error)
}

// ObjectInfo is the metadata of a resource.
type ObjectInfo struct {
	// URI holds the URI used for the query as is (the path, for local files).
	URI string
	// Size is the number of bytes.
	Size int64
	// ModTime is the last modification time.
	ModTime time.Time
	// ContentType is the MIME type. It is empty on the local file system.
	ContentType string
	// Metadata is the user-defined metadata written with WithMetadata.
	// It is always nil on the local file system.
	// S3 normalizes keys to lower case, so their case may differ from what was written.
	Metadata map[string]string
}

// Entry is one item returned by List.
//
// Passing only a string to a callback makes the caller re-decide whether it is a
// pseudo-directory by "does it end with the delimiter". Handlers have typed
// information — attrs.Prefix, CommonPrefixes — so it is passed on without
// flattening it.
type Entry struct {
	// URI is the full URI. It can be passed straight to the root Store.
	URI string
	// Name is relative to the listed prefix.
	// It can be passed straight to a scoped store (Store.Sub).
	Name string
	// IsPrefix reports that this is a pseudo-directory (a hierarchy folded by the delimiter).
	// It can be true only in a listing with a delimiter.
	IsPrefix bool
	// Size is the number of bytes. It is 0 when IsPrefix is true.
	Size int64
	// ModTime is the last modification time. It is the zero value when IsPrefix is true.
	ModTime time.Time
}
