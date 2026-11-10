// Package memio provides an in-memory remoteio.Handler.
//
// It is meant to stand in for remote storage in tests. It needs no docker,
// credentials or network, and meets the same contract as the GCS / S3 handlers
// (running it through storetest.TestHandler is the evidence).
//
// Before it existed, every consuming repository hand-wrote fakes for Open / List /
// Write. Across the ecosystem there were 72 fake methods in 21 test files, each
// reimplementing the folding of pseudo-directories by delimiter — a state in which CI
// stays green even when they drift from the real thing.
package memio

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"iter"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/shouni/go-remote-io/remoteio"
)

// DefaultScheme is the scheme name handled when none is specified.
const DefaultScheme = "mem"

// Handler is an in-memory remoteio.Handler.
// It also implements server-side copy (remoteio.Copier).
//
// It is safe for concurrent use. Since it stands in for remote storage, it would be
// pointless if callers could not write tests that hit it concurrently.
type Handler struct {
	scheme string

	mu      sync.RWMutex
	objects map[string]object

	// failOn is a hook for making operations fail on purpose.
	// It is there to exercise paths that the happy path never takes, such as "how does
	// the caller behave when a write fails part-way".
	failOn func(op, uri string) error
	// now is the source of time, so tests can pin modification times.
	now func() time.Time
}

// object is one stored item.
type object struct {
	data               []byte
	contentType        string
	cacheControl       string
	contentDisposition string
	metadata           map[string]string
	modTime            time.Time
}

var (
	_ remoteio.Handler = (*Handler)(nil)
	_ remoteio.Copier  = (*Handler)(nil)
)

// Option is a functional option that changes how a Handler is created.
type Option func(*Handler)

// WithScheme sets the scheme name handled (DefaultScheme by default).
//
// Pass gs or s3 and it behaves as a handler for that scheme, so consumers' tests can
// write the same URIs as production.
func WithScheme(scheme string) Option {
	return func(h *Handler) { h.scheme = scheme }
}

// WithFailure registers a hook that makes operations fail on purpose.
// op is one of "open" / "stat" / "list" / "write" / "delete" / "copy".
// Returning nil processes the operation as usual.
func WithFailure(fail func(op, uri string) error) Option {
	return func(h *Handler) { h.failOn = fail }
}

// WithClock replaces the source of time. It is for tests that check modification times.
func WithClock(now func() time.Time) Option {
	return func(h *Handler) { h.now = now }
}

// New returns an in-memory handler.
func New(opts ...Option) *Handler {
	h := &Handler{
		scheme:  DefaultScheme,
		objects: make(map[string]object),
		now:     time.Now,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(h)
		}
	}
	return h
}

// Scheme returns the scheme name handled.
func (h *Handler) Scheme() string { return h.scheme }

// Seed loads contents without going through Write.
// It is for setting up test preconditions, and is not affected by WithFailure.
//
// Use Write when metadata is needed too. Setting up preconditions almost always needs
// only the contents, so no arguments were added here.
func (h *Handler) Seed(uri string, data []byte) error {
	bucket, key, err := h.parseObject(uri)
	if err != nil {
		return err
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	h.objects[path(bucket, key)] = h.newObject(data, remoteio.WriteOptions{
		ContentType: remoteio.DefaultContentType,
	})
	return nil
}

// URIs returns the URIs of all stored objects in lexical order.
// It is for checking in tests "where things were written".
func (h *Handler) URIs() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	out := make([]string, 0, len(h.objects))
	for key := range h.objects {
		bucket, object, _ := strings.Cut(key, "/")
		out = append(out, remoteio.BuildURI(h.scheme, bucket, object))
	}
	slices.Sort(out)
	return out
}

// Options returns the WriteOptions resolved when the object was stored.
//
// It is for checking that Content-Type and Cache-Control were passed as intended.
// Only Content-Type and Metadata appear on ObjectInfo; Cache-Control and
// Content-Disposition cannot be observed from outside the abstraction. Consumers wrote
// fakes that kept "the last settings passed" to verify them, so this takes that on.
func (h *Handler) Options(uri string) (remoteio.WriteOptions, bool) {
	bucket, key, err := h.parseObject(uri)
	if err != nil {
		return remoteio.WriteOptions{}, false
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	obj, ok := h.objects[path(bucket, key)]
	if !ok {
		return remoteio.WriteOptions{}, false
	}
	return remoteio.WriteOptions{
		ContentType:        obj.contentType,
		CacheControl:       obj.cacheControl,
		ContentDisposition: obj.contentDisposition,
		Metadata:           maps.Clone(obj.metadata),
	}, true
}

// Len returns the number of stored objects.
func (h *Handler) Len() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.objects)
}

// Open returns a read stream for the stored contents.
func (h *Handler) Open(_ context.Context, uri string) (io.ReadCloser, error) {
	bucket, key, err := h.parseObject(uri)
	if err != nil {
		return nil, err
	}
	if err := h.fail("open", uri); err != nil {
		return nil, err
	}

	h.mu.RLock()
	obj, ok := h.objects[path(bucket, key)]
	h.mu.RUnlock()
	if !ok {
		return nil, notExist(uri)
	}
	// Handing out the stored slice would let the reader modify it.
	return io.NopCloser(bytes.NewReader(slices.Clone(obj.data))), nil
}

// Stat returns metadata.
func (h *Handler) Stat(_ context.Context, uri string) (remoteio.ObjectInfo, error) {
	bucket, key, err := h.parseObject(uri)
	if err != nil {
		return remoteio.ObjectInfo{}, err
	}
	if err := h.fail("stat", uri); err != nil {
		return remoteio.ObjectInfo{}, err
	}

	h.mu.RLock()
	obj, ok := h.objects[path(bucket, key)]
	h.mu.RUnlock()
	if !ok {
		return remoteio.ObjectInfo{}, notExist(uri)
	}

	return remoteio.ObjectInfo{
		URI:         uri,
		Size:        int64(len(obj.data)),
		ModTime:     obj.modTime,
		ContentType: obj.contentType,
		Metadata:    maps.Clone(obj.metadata),
	}, nil
}

// List enumerates what is under the prefix.
//
// Delimiter handling matches GCS / S3. If this drifted from the real thing, every test
// using this package would lose its meaning, so it is the crux of the implementation.
func (h *Handler) List(_ context.Context, uri string, opts remoteio.ListOptions) iter.Seq2[remoteio.Entry, error] {
	uri = remoteio.ListPrefix(uri)

	bucket, prefix, err := h.parseBucket(uri)
	if err != nil {
		return errSeq(err)
	}
	if err := h.fail("list", uri); err != nil {
		return errSeq(err)
	}

	h.mu.RLock()
	keys := make([]string, 0, len(h.objects))
	snapshot := make(map[string]object, len(h.objects))
	for key, obj := range h.objects {
		keys = append(keys, key)
		snapshot[key] = obj
	}
	h.mu.RUnlock()
	slices.Sort(keys)

	base := path(bucket, prefix)
	return func(yield func(remoteio.Entry, error) bool) {
		seen := make(map[string]bool)
		for _, key := range keys {
			if !strings.HasPrefix(key, base) {
				continue
			}
			rest := strings.TrimPrefix(key, base)
			if rest == "" {
				continue
			}

			if opts.Delimiter != "" {
				if i := strings.Index(rest, opts.Delimiter); i >= 0 {
					// Fold what follows the delimiter into a pseudo-directory.
					name := rest[:i+len(opts.Delimiter)]
					if seen[name] {
						continue
					}
					seen[name] = true
					entry := remoteio.Entry{
						URI:      remoteio.BuildURI(h.scheme, bucket, prefix+name),
						Name:     name,
						IsPrefix: true,
					}
					if !yield(entry, nil) {
						return
					}
					continue
				}
			}

			obj := snapshot[key]
			entry := remoteio.Entry{
				URI:     remoteio.BuildURI(h.scheme, bucket, prefix+rest),
				Name:    rest,
				Size:    int64(len(obj.data)),
				ModTime: obj.modTime,
			}
			if !yield(entry, nil) {
				return
			}
		}
	}
}

// Write stores the contents.
//
// It reads everything before registering it, so stored contents do not change even
// when it fails part-way (meeting Handler's "the destination does not change unless
// the write succeeds").
func (h *Handler) Write(_ context.Context, uri string, src io.Reader, opts remoteio.WriteOptions) error {
	bucket, key, err := h.parseObject(uri)
	if err != nil {
		return err
	}
	if err := h.fail("write", uri); err != nil {
		return err
	}

	data, err := io.ReadAll(src)
	if err != nil {
		return fmt.Errorf("write to memory (URI: %s): %w", uri, err)
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	full := path(bucket, key)
	if opts.IfNotExists {
		if _, ok := h.objects[full]; ok {
			return fmt.Errorf("object already exists (URI: %s): %w", uri, remoteio.ErrExist)
		}
	}
	h.objects[full] = h.newObject(data, opts)
	return nil
}

// Delete removes an object. Absence is not an error.
func (h *Handler) Delete(_ context.Context, uri string) error {
	bucket, key, err := h.parseObject(uri)
	if err != nil {
		return err
	}
	if err := h.fail("delete", uri); err != nil {
		return err
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.objects, path(bucket, key))
	return nil
}

// CopyTo duplicates stored contents. It implements remoteio.Copier.
func (h *Handler) CopyTo(_ context.Context, src, dst string) error {
	srcBucket, srcKey, err := h.parseObject(src)
	if err != nil {
		return err
	}
	dstBucket, dstKey, err := h.parseObject(dst)
	if err != nil {
		return err
	}
	if err := h.fail("copy", src); err != nil {
		return err
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	obj, ok := h.objects[path(srcBucket, srcKey)]
	if !ok {
		return notExist(src)
	}
	obj.data = slices.Clone(obj.data)
	obj.metadata = maps.Clone(obj.metadata)
	obj.modTime = h.now()
	h.objects[path(dstBucket, dstKey)] = obj
	return nil
}

// newObject builds the value to store. Keeping the map the caller passed as is would
// let it be modified later, so it is copied.
func (h *Handler) newObject(data []byte, opts remoteio.WriteOptions) object {
	return object{
		data:               slices.Clone(data),
		contentType:        opts.ContentType,
		cacheControl:       opts.CacheControl,
		contentDisposition: opts.ContentDisposition,
		metadata:           maps.Clone(opts.Metadata),
		modTime:            h.now(),
	}
}

func (h *Handler) fail(op, uri string) error {
	if h.failOn == nil {
		return nil
	}
	return h.failOn(op, uri)
}

func (h *Handler) parseBucket(uri string) (bucket, prefix string, err error) {
	return remoteio.ParseBucketURI(h.scheme, uri)
}

func (h *Handler) parseObject(uri string) (bucket, key string, err error) {
	return remoteio.ParseObjectURI(h.scheme, uri)
}

// path is the storage key. To avoid prefix matches across buckets, it is one string
// joining the bucket name and the object name with "/".
func path(bucket, key string) string { return bucket + "/" + key }

func notExist(uri string) error {
	return fmt.Errorf("object not found (URI: %s): %w", uri, remoteio.ErrNotExist)
}

func errSeq(err error) iter.Seq2[remoteio.Entry, error] {
	return func(yield func(remoteio.Entry, error) bool) { yield(remoteio.Entry{}, err) }
}
