// Package remoteio is a library that handles local files, GCS and S3 behind a single
// interface.
//
// The entry point is Store. Take one from the factory returned by gcs.New / s3.New,
// or assemble one yourself by lining up Handler values in NewStore.
//
//	factory, err := gcs.New(ctx)
//	defer factory.Close()
//	store, err := factory.Store()
//
//	jobs := store.Sub("gs://my-bucket/jobs")   // decide the scope once, at startup
//	err = jobs.Write(ctx, jobID+"/status.json", body, remoteio.WithContentType("application/json"))
//
// Extending to a new storage backend means implementing only Handler.
//
// # Remote URIs are not URLs
//
// gs:// and s3:// are "scheme + bucket + raw key", not URLs. An object name is an
// arbitrary byte sequence, and spaces and ? are valid characters, but through
// net/url they turn into %20 and a query separator. ParseURI does not decode, so a
// mangled URI points at a different key without an error. Use neither net/url nor
// filepath for path operations; go through this package's Dir / Join / BuildURI.
package remoteio

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"strings"
	"time"
)

// Reader is a minimal consumer-facing interface dedicated to reading a single resource.
//
// Functions that want a narrower dependency should take this. Requiring the whole
// Store makes the caller's tests provide operations they never use.
type Reader interface {
	// Open returns an io.ReadCloser for the given path.
	// When the target does not exist, the error contains ErrNotExist.
	Open(ctx context.Context, name string) (io.ReadCloser, error)
}

// Writer is a minimal consumer-facing interface dedicated to writing a single resource.
type Writer interface {
	// Write writes the contents of src to the given path.
	// Unless it succeeds, the destination does not change.
	Write(ctx context.Context, name string, src io.Reader, opts ...WriteOption) error
}

// Store is the entry point for reading and writing within one scope.
//
// The root Store (the return value of NewStore) accepts full URIs and local paths.
// A store obtained from Sub is pinned to a prefix and accepts only names relative to it.
//
// Reading, writing and signing are combined because consumers almost always need all
// three. As separate interfaces, consumers would hold a type whose only job is to bundle them.
type Store interface {
	Reader
	Writer

	// Stat returns metadata. When the target is missing, the error contains ErrNotExist.
	Stat(ctx context.Context, name string) (ObjectInfo, error)

	// Exists reports whether the target exists. Absence is (false, nil).
	//
	// It looks only at "is there an object". Neither local directories nor remote
	// pseudo-directories count. Remote storage has no directory entity, so if local
	// alone returned true, the same call would mean different things per scheme. Use
	// List to find out whether a hierarchy exists.
	Exists(ctx context.Context, name string) (bool, error)

	// List enumerates what is under name. The prefix is always normalized to the form
	// that means "the contents of that level", so "data" and "data/" give the same
	// result, and "data-archive/" does not match.
	//
	// Listing a prefix with nothing under it returns empty rather than an error. Remote
	// storage has no state such as "a prefix that does not exist", so making it an error
	// for local alone would make the same call mean different things per scheme. It
	// cannot be used to check whether a directory exists.
	//
	// A listing without a delimiter returns everything under the prefix recursively.
	// With 3 artifacts per job, the caller receives 3 times the data and deduplicates it
	// itself. If only the direct children are needed, move that walk to the server with
	// WithDelimiter("/").
	//
	//	for entry, err := range store.List(ctx, "jobs", remoteio.WithDelimiter("/")) {
	//		if err != nil {
	//			return err
	//		}
	//		if entry.IsPrefix {
	//			...
	//		}
	//	}
	List(ctx context.Context, name string, opts ...ListOption) iter.Seq2[Entry, error]

	// Delete removes one target object. Absence is not an error (idempotent).
	//
	// Given a prefix, nothing under it is removed: it silently succeeds as "there is no
	// object with that name". Use DeletePrefix to remove everything under a prefix.
	Delete(ctx context.Context, name string) error

	// Copy duplicates the contents of src into dst. It can cross schemes.
	//
	// When both resolve to the same handler and that handler implements Copier, it is a
	// server-side copy. Otherwise it relays a stream.
	// The caller needs no branching.
	Copy(ctx context.Context, src, dst string, opts ...WriteOption) error

	// SignURL generates a signed URL.
	// When the responsible handler does not implement Signer, the error contains ErrNotSupported.
	SignURL(ctx context.Context, name, method string, expires time.Duration) (string, error)

	// Sub returns a store pinned to the prefix.
	//
	// Instead of carrying the bucket name into every call, it is decided once at
	// assembly time. The resulting store does not accept absolute URIs with a scheme;
	// passing one is an error containing ErrAbsoluteName (to keep code that thinks it
	// narrowed its scope from writing to another bucket).
	Sub(prefix string) Store
}

// ReadAll reads and returns the whole contents of name.
// It saves writing the Open and Close pair every time.
func ReadAll(ctx context.Context, r Reader, name string) ([]byte, error) {
	rc, err := r.Open(ctx, name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, wrapf(err, "read (%s)", name)
	}
	return data, nil
}

// WriteAll writes data to name.
func WriteAll(ctx context.Context, w Writer, name string, data []byte, opts ...WriteOption) error {
	return w.Write(ctx, name, bytes.NewReader(data), opts...)
}

// PrefixDeleter is the minimal interface DeletePrefix requires. Store satisfies it.
type PrefixDeleter interface {
	List(ctx context.Context, name string, opts ...ListOption) iter.Seq2[Entry, error]
	Delete(ctx context.Context, name string) error
}

// DeletePrefix deletes every object under prefix and returns how many it deleted.
//
// Delete removes a single object, and given a prefix it silently succeeds as "there
// is no object with that name". On storage without a directory entity, the deleter can
// only list and delete one by one, and when each consumer writes that procedure,
// copies multiply that differ slightly in range checks and failure handling.
//
// prefix means the same as in List: "data" and "data/" cover the same range, and
// "data-archive/" is not included. Passing the empty string to a scoped store
// (Store.Sub) targets that whole scope. A URI with a scheme and an empty object name
// (the bucket root) cannot bound the range, so it is rejected with ErrInvalidURI.
//
// If listing fails, it returns without deleting anything. Failures of individual
// deletes are collected and returned with errors.Join, and the remaining deletes
// continue. A prefix with nothing under it is (0, nil).
func DeletePrefix(ctx context.Context, s PrefixDeleter, prefix string) (deleted int, err error) {
	if scheme := Scheme(prefix); scheme != "" {
		if _, _, object, err := ParseURI(prefix); err != nil {
			return 0, err
		} else if strings.Trim(object, "/") == "" {
			return 0, fmt.Errorf("%w: the bucket root cannot be a deletion range (%s)", ErrInvalidURI, prefix)
		}
	}

	// Collect first, then delete. Deleting during the listing can miss entries in implementations that paginate.
	var names []string
	for entry, err := range s.List(ctx, prefix) {
		if err != nil {
			return 0, wrapf(err, "list deletion targets (%s)", prefix)
		}
		if entry.IsPrefix {
			continue
		}
		// Use Name, not Entry.URI. The URI is ErrAbsoluteName on a scoped store.
		names = append(names, Join(prefix, entry.Name))
	}

	var errs []error
	for _, name := range names {
		if err := s.Delete(ctx, name); err != nil {
			errs = append(errs, wrapf(err, "delete %s", name))
			continue
		}
		deleted++
	}
	return deleted, errors.Join(errs...)
}

// Factory owns the lifecycle of a storage client and can hand out a Store and a
// Handler from it. gcs.ClientFactory and s3.ClientFactory implement it.
//
// It also returns the Handler because bundling several clouds into one Store needs
// the responsible scheme. If it returned only a Store, the bundling side would need
// another way to learn "which scheme is this factory".
type Factory interface {
	io.Closer

	// Store returns a Store that handles this factory's scheme and local paths.
	Store() (Store, error)

	// Handler returns the handler for the scheme this factory is responsible for.
	// Use it to bundle several clouds into one Store.
	Handler() (Handler, error)
}
