package remoteio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"maps"
	"slices"
	"strings"
	"time"
)

// Router is a Store that looks at a URI's scheme and delegates to the matching Handler.
//
// The dispatch is at run time because paths come from configuration and user input,
// so which scheme will be handled cannot be decided statically. "Which schemes are
// supported", on the other hand, is decided at construction, so it is held explicitly
// as a set of handlers. The set does not change after construction, so a Router is
// safe for concurrent use.
type Router struct {
	handlers map[string]Handler
	fallback Handler
}

var _ Store = (*Router)(nil)

// NewRouter returns a Router with the given handlers registered under their schemes.
// A handler whose Scheme() is empty becomes the fallback (usually the local file
// system) for paths that match no scheme.
// When the same scheme is passed more than once, the last one wins.
func NewRouter(handlers ...Handler) *Router {
	r := &Router{handlers: make(map[string]Handler, len(handlers))}
	for _, h := range handlers {
		if h == nil {
			continue
		}
		if scheme := h.Scheme(); scheme != "" {
			r.handlers[scheme] = h
			continue
		}
		r.fallback = h
	}
	return r
}

// NewStore returns a Store with the local handlers registered alongside the remote
// handlers.
//
// Local (no scheme) and file:// are always paired so that the same store can also
// read local files during development. Remote schemes nobody is responsible for are
// not registered, so they are rejected clearly as unsupported.
func NewStore(handlers ...Handler) *Router {
	all := make([]Handler, 0, len(handlers)+2)
	all = append(all, handlers...)
	all = append(all, NewLocalHandler(), NewFileHandler())
	return NewRouter(all...)
}

// Schemes returns the registered scheme names (the fallback is not included).
// They are returned in lexical order so the order does not change between calls.
func (r *Router) Schemes() []string {
	return slices.Sorted(maps.Keys(r.handlers))
}

// resolve returns the handler responsible for uri.
// This is the only place that decides a scheme is unsupported.
func (r *Router) resolve(uri string) (Handler, error) {
	scheme := Scheme(uri)
	if scheme == "" {
		if r.fallback == nil {
			return nil, fmt.Errorf("%w: no handler registered for local paths (%s)", ErrUnsupportedScheme, uri)
		}
		return r.fallback, nil
	}
	handler, ok := r.handlers[scheme]
	if !ok {
		return nil, fmt.Errorf("%w: %s (%s)", ErrUnsupportedScheme, scheme, uri)
	}
	return handler, nil
}

// Open returns a read stream for the given path.
func (r *Router) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	return r.dispatch(name, func(h Handler) (io.ReadCloser, error) {
		return h.Open(ctx, name)
	})
}

// Stat returns the metadata of the given path.
func (r *Router) Stat(ctx context.Context, name string) (ObjectInfo, error) {
	return r.dispatch(name, func(h Handler) (ObjectInfo, error) {
		return h.Stat(ctx, name)
	})
}

// Exists reports whether an object exists at the given path.
//
// Handler does not have Exists and it is derived from Stat instead, to remove the
// room for each implementation to get the "absence returns (false, nil)" promise wrong.
func (r *Router) Exists(ctx context.Context, name string) (bool, error) {
	if _, err := r.Stat(ctx, name); err != nil {
		if errors.Is(err, ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// List enumerates what is under the given path.
//
// A prefix with nothing under it returns empty rather than an error, and without a
// delimiter everything below is returned recursively. See Store.List for details.
func (r *Router) List(ctx context.Context, name string, opts ...ListOption) iter.Seq2[Entry, error] {
	handler, err := r.resolve(name)
	if err != nil {
		return errSeq(err)
	}
	return handler.List(ctx, ListPrefix(name), newListOptions(opts...))
}

// Write writes data to the given path.
func (r *Router) Write(ctx context.Context, name string, src io.Reader, opts ...WriteOption) error {
	handler, err := r.resolve(name)
	if err != nil {
		return err
	}
	return handler.Write(ctx, name, src, newWriteOptions(opts...))
}

// Delete removes the resource at the given path.
func (r *Router) Delete(ctx context.Context, name string) error {
	handler, err := r.resolve(name)
	if err != nil {
		return err
	}
	return handler.Delete(ctx, name)
}

// Copy duplicates the contents of src into dst.
//
// When both resolve to the same handler and that handler implements Copier, it falls
// through to a server-side copy. Otherwise it relays a stream.
//
// With a WriteOption it always relays a stream. CopyTo takes no options, so falling
// through to a server-side copy would silently drop them (WithIfNotExists given, yet
// the target is overwritten, and so on).
//
// On either path, the source's Content-Type and user-defined metadata are carried
// over. A server-side copy keeps the original attributes, so if only the stream relay
// rewrote them with the default Content-Type, the same Copy would give different
// results depending on the scheme pair.
func (r *Router) Copy(ctx context.Context, src, dst string, opts ...WriteOption) error {
	srcHandler, err := r.resolve(src)
	if err != nil {
		return err
	}
	dstHandler, err := r.resolve(dst)
	if err != nil {
		return err
	}

	// Decide by whether the scheme is the same. Comparing the handler values themselves
	// is not used because it panics when they contain an incomparable type.
	if len(opts) == 0 && srcHandler.Scheme() == dstHandler.Scheme() {
		if copier, ok := srcHandler.(Copier); ok {
			err := copier.CopyTo(ctx, src, dst)
			// Only ErrNotSupported falls back to the stream relay. Whether a server-side copy
			// is possible can depend on the target (S3's CopyObject cannot be used above 5GB),
			// and if implementations could not report that at run time, callers would have to
			// carry "a different way of writing it, only when large".
			if err == nil {
				return nil
			}
			if !errors.Is(err, ErrNotSupported) {
				return wrapf(err, "copy (%s -> %s)", src, dst)
			}
		}
	}

	writeOpts := newWriteOptions(opts...)
	carrySourceAttributes(ctx, srcHandler, src, &writeOpts)

	rc, err := srcHandler.Open(ctx, src)
	if err != nil {
		return wrapf(err, "open copy source (%s)", src)
	}
	defer func() { _ = rc.Close() }()

	if err := dstHandler.Write(ctx, dst, rc, writeOpts); err != nil {
		return wrapf(err, "write copy destination (%s -> %s)", src, dst)
	}
	return nil
}

// carrySourceAttributes carries over from the source the Content-Type and metadata the
// caller did not specify.
//
// It adds one Stat round trip, but without it a stream-relayed copy paints over the
// original Content-Type with the default (duplicating an image yields text/plain).
// When Stat fails it just gives up carrying them over and continues the copy itself:
// failing the copy over missing attributes would be more than the caller asked for.
func carrySourceAttributes(ctx context.Context, h Handler, src string, o *WriteOptions) {
	// Whether the caller set a value explicitly is decided by comparing with the defaults with no options.
	defaults := newWriteOptions()
	needType := o.ContentType == defaults.ContentType
	needMeta := len(o.Metadata) == 0
	if !needType && !needMeta {
		return
	}

	info, err := h.Stat(ctx, src)
	if err != nil {
		return
	}
	if needType && info.ContentType != "" {
		o.ContentType = info.ContentType
	}
	if needMeta && len(info.Metadata) > 0 {
		o.Metadata = maps.Clone(info.Metadata)
	}
}

// SignURL generates a signed URL.
func (r *Router) SignURL(ctx context.Context, name, method string, expires time.Duration) (string, error) {
	handler, err := r.resolve(name)
	if err != nil {
		return "", err
	}
	signer, ok := handler.(Signer)
	if !ok {
		return "", fmt.Errorf("%w: signed URL (%s)", ErrNotSupported, name)
	}
	return signer.SignURL(ctx, name, method, expires)
}

// Sub returns a store pinned to the prefix.
func (r *Router) Sub(prefix string) Store {
	return Sub(r, prefix)
}

// Sub returns a store that pins any Store to the prefix.
//
// Types that wrap a Store to add behaviour (such as test fakes that record calls)
// should delegate their Sub method to this function. A Sub promoted from an embedded
// field uses the embedded value as the base of the scope, so the wrapper's behaviour
// is lost beyond the scope.
//
//	func (d *decorator) Sub(prefix string) remoteio.Store {
//		return remoteio.Sub(d, prefix)
//	}
func Sub(store Store, prefix string) Store {
	return &scopedStore{root: store, prefix: prefix}
}

// dispatch resolves the handler and delegates to an operation that returns a value.
// It exists only to make the return value on a resolution failure a uniform zero value.
//
// Because methods can have type parameters, it can be written as a method rather
// than a function taking the receiver as its first argument.
func (r *Router) dispatch[T any](uri string, fn func(Handler) (T, error)) (T, error) {
	handler, err := r.resolve(uri)
	if err != nil {
		var zero T
		return zero, err
	}
	return fn(handler)
}

// errSeq returns an iterator that reports, once, a failure before iteration started.
func errSeq(err error) iter.Seq2[Entry, error] {
	return func(yield func(Entry, error) bool) { yield(Entry{}, err) }
}

// scopedStore is a Store pinned to a prefix.
//
// Instead of carrying the bucket name into every call, it is decided once at assembly
// time. Without it, consumers rewrite a thin method building a URI from "bucket name +
// path" in every repository. That is what happened, and even that was not enough:
// consumers hard-coded "gs://" in many places.
type scopedStore struct {
	// root is the Store interface. Pinning it to the concrete Router would let a type
	// wrapping a Store be bypassed the moment it goes through Sub.
	root   Store
	prefix string
}

var _ Store = (*scopedStore)(nil)

// resolveName converts a relative name into a full path within the scope.
//
// Absolute URIs with a scheme are rejected to keep code that thinks it narrowed its
// scope from writing to another bucket.
func (s *scopedStore) resolveName(name string) (string, error) {
	if Scheme(name) != "" {
		return "", fmt.Errorf("%w: %s (scope: %s)", ErrAbsoluteName, name, s.prefix)
	}
	if hasRelativeSegment(name) {
		return "", fmt.Errorf("%w: name contains a relative segment: %s (scope: %s)", ErrInvalidURI, name, s.prefix)
	}
	return Join(s.prefix, name), nil
}

// hasRelativeSegment reports whether a name contains a "." or ".." segment.
//
// The local Join is filepath.Join, which folds ".." and can point outside the scope
// (exactly the "can write somewhere else" that ErrAbsoluteName is meant to prevent).
// Remotely the scope is not escaped, but a literal key gs://b/jobs/../x.txt is
// created, and the same call means different things per scheme. Both are rejected
// together.
func hasRelativeSegment(name string) bool {
	for segment := range strings.SplitSeq(strings.ReplaceAll(name, `\`, "/"), "/") {
		if segment == "." || segment == ".." {
			return true
		}
	}
	return false
}

func (s *scopedStore) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	full, err := s.resolveName(name)
	if err != nil {
		return nil, err
	}
	return s.root.Open(ctx, full)
}

func (s *scopedStore) Stat(ctx context.Context, name string) (ObjectInfo, error) {
	full, err := s.resolveName(name)
	if err != nil {
		return ObjectInfo{}, err
	}
	return s.root.Stat(ctx, full)
}

func (s *scopedStore) Exists(ctx context.Context, name string) (bool, error) {
	full, err := s.resolveName(name)
	if err != nil {
		return false, err
	}
	return s.root.Exists(ctx, full)
}

func (s *scopedStore) List(ctx context.Context, name string, opts ...ListOption) iter.Seq2[Entry, error] {
	full, err := s.resolveName(name)
	if err != nil {
		return errSeq(err)
	}
	return s.root.List(ctx, full, opts...)
}

func (s *scopedStore) Write(ctx context.Context, name string, src io.Reader, opts ...WriteOption) error {
	full, err := s.resolveName(name)
	if err != nil {
		return err
	}
	return s.root.Write(ctx, full, src, opts...)
}

func (s *scopedStore) Delete(ctx context.Context, name string) error {
	full, err := s.resolveName(name)
	if err != nil {
		return err
	}
	return s.root.Delete(ctx, full)
}

func (s *scopedStore) Copy(ctx context.Context, src, dst string, opts ...WriteOption) error {
	fullSrc, err := s.resolveName(src)
	if err != nil {
		return err
	}
	fullDst, err := s.resolveName(dst)
	if err != nil {
		return err
	}
	return s.root.Copy(ctx, fullSrc, fullDst, opts...)
}

func (s *scopedStore) SignURL(ctx context.Context, name, method string, expires time.Duration) (string, error) {
	full, err := s.resolveName(name)
	if err != nil {
		return "", err
	}
	return s.root.SignURL(ctx, full, method, expires)
}

// Sub returns a store with the scope narrowed further.
func (s *scopedStore) Sub(prefix string) Store {
	return Sub(s.root, Join(s.prefix, prefix))
}
