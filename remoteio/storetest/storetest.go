// Package storetest provides a conformance test suite that checks whether a
// remoteio.Handler implementation meets its contract.
//
// It follows the same idea as TestFS in testing/fstest. GCS, S3, local, memio and
// third-party implementations all go through the same single suite.
//
//	func TestConformance(t *testing.T) {
//		storetest.TestHandler(t, func(t *testing.T) storetest.Fixture {
//			return storetest.Fixture{Handler: memio.New(), Root: "mem://bucket/conformance"}
//		})
//	}
//
// Without it, CI stays green even when a fake and the real thing drift apart. In fact,
// mismatches such as "a listing without a delimiter means different things per scheme"
// and "only local makes Exists true for a directory" stayed outside the tests for a
// long time.
package storetest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/shouni/go-remote-io/remoteio"
)

// Fixture is the scaffolding the suite needs to exercise one Handler.
type Fixture struct {
	// Handler is what is being checked.
	Handler remoteio.Handler

	// Root is a place the suite may freely read and write.
	// Pass "gs://bucket/conformance" for remote storage, or a t.TempDir() value for local.
	// The suite touches nothing outside it.
	Root string

	// SupportsContentType reports whether Content-Type can be stored and returned by Stat.
	// The local file system cannot keep it, so it is false there.
	SupportsContentType bool

	// SupportsMetadata reports whether user-defined metadata can be stored and returned by Stat.
	SupportsMetadata bool

	// SupportsIfNotExists reports whether conditional writes are supported.
	// Set it to false when a fake does not interpret conditional requests
	// (it is a difference from the real thing, so declare it rather than letting it pass silently).
	SupportsIfNotExists bool

	// BucketScoped reports whether URIs have the form "scheme + bucket + key".
	// gs:// / s3:// are true; file:// has a scheme but no notion of a bucket, so it is
	// false. It selects what the "reject URIs with an empty object name" check applies to.
	BucketScoped bool
}

// TestHandler checks the Handler contract.
//
// newFixture is called for each subtest. Return fresh scaffolding every time, so that
// what one subtest wrote does not leak into the next.
func TestHandler(t *testing.T, newFixture func(t *testing.T) Fixture) {
	t.Helper()

	run := func(name string, fn func(t *testing.T, f Fixture)) {
		t.Run(name, func(t *testing.T) {
			fn(t, newFixture(t))
		})
	}

	run("Scheme is stable across calls", testSchemeIsStable)
	run("missing wraps ErrNotExist", testNotExist)
	run("reads back what was written", testWriteReadRoundTrip)
	run("Stat returns size and URI", testStatMetadata)
	run("Delete is idempotent", testDeleteIsIdempotent)
	run("list with delimiter is shallow", testListShallow)
	run("list without delimiter is recursive", testListRecursive)
	run("list prefix is always normalized", testListNormalizesPrefix)
	run("list of empty prefix is empty without error", testListEmptyPrefix)
	run("list stops on break", testListStops)
	run("Entry.URI opens as is", testEntryURIIsUsable)
	run("failed write leaves the target unchanged", testWriteIsAtomic)
	run("conditional write protects the existing object", testWriteIfNotExists)
	run("rejects a foreign scheme", testRejectsForeignScheme)
	run("rejects a URI with an empty object name", testRejectsEmptyObjectName)
}

// uri builds a name relative to Root into the form the handler receives.
func (f Fixture) uri(name string) string { return remoteio.Join(f.Root, name) }

// write is a write for setting up preconditions.
func (f Fixture) write(t *testing.T, name, content string) string {
	t.Helper()
	full := f.uri(name)
	err := f.Handler.Write(context.Background(), full, strings.NewReader(content),
		remoteio.WriteOptions{ContentType: remoteio.DefaultContentType})
	if err != nil {
		t.Fatalf("precondition write failed (%s): %v", full, err)
	}
	return full
}

// read reads the whole contents from the handler.
func (f Fixture) read(t *testing.T, uri string) string {
	t.Helper()
	rc, err := f.Handler.Open(context.Background(), uri)
	if err != nil {
		t.Fatalf("Open failed (%s): %v", uri, err)
	}
	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read failed (%s): %v", uri, err)
	}
	return string(data)
}

// collect reads the whole listing.
func (f Fixture) collect(t *testing.T, uri string, opts remoteio.ListOptions) []remoteio.Entry {
	t.Helper()
	var out []remoteio.Entry
	for entry, err := range f.Handler.List(context.Background(), uri, opts) {
		if err != nil {
			t.Fatalf("List failed (%s): %v", uri, err)
		}
		out = append(out, entry)
	}
	return out
}

func names(entries []remoteio.Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name)
	}
	return out
}

func testSchemeIsStable(t *testing.T, f Fixture) {
	if got, want := f.Handler.Scheme(), f.Handler.Scheme(); got != want {
		t.Fatalf("Scheme() changes between calls: %q and %q", got, want)
	}
	if strings.Contains(f.Handler.Scheme(), "://") {
		t.Errorf("Scheme() must return a name without the separator: %q", f.Handler.Scheme())
	}
}

// That callers can detect absence with errors.Is whatever the scheme is the
// condition for this abstraction to work.
func testNotExist(t *testing.T, f Fixture) {
	ctx := context.Background()
	missing := f.uri("missing.txt")

	if _, err := f.Handler.Open(ctx, missing); !errors.Is(err, remoteio.ErrNotExist) {
		t.Errorf("Open's not-exist error does not contain ErrNotExist: %v", err)
	}
	if _, err := f.Handler.Stat(ctx, missing); !errors.Is(err, remoteio.ErrNotExist) {
		t.Errorf("Stat's not-exist error does not contain ErrNotExist: %v", err)
	}
}

func testWriteReadRoundTrip(t *testing.T, f Fixture) {
	uri := f.write(t, "round/trip.txt", "hello")
	if got := f.read(t, uri); got != "hello" {
		t.Errorf("contents read back differ: got %q, want %q", got, "hello")
	}
}

func testStatMetadata(t *testing.T, f Fixture) {
	ctx := context.Background()
	uri := f.uri("meta.txt")

	opts := remoteio.WriteOptions{ContentType: "application/json", Metadata: map[string]string{"job-id": "j1"}}
	if err := f.Handler.Write(ctx, uri, strings.NewReader(`{"a":1}`), opts); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	info, err := f.Handler.Stat(ctx, uri)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if info.URI != uri {
		t.Errorf("ObjectInfo.URI must return the URI used for the query: got %q, want %q", info.URI, uri)
	}
	if info.Size != int64(len(`{"a":1}`)) {
		t.Errorf("Size differs: got %d, want %d", info.Size, len(`{"a":1}`))
	}
	if f.SupportsContentType && info.ContentType != "application/json" {
		t.Errorf("ContentType was not stored: got %q", info.ContentType)
	}
	if f.SupportsMetadata && info.Metadata["job-id"] != "j1" {
		t.Errorf("Metadata was not stored: got %v", info.Metadata)
	}
}

func testDeleteIsIdempotent(t *testing.T, f Fixture) {
	ctx := context.Background()
	uri := f.write(t, "delete-me.txt", "x")

	if err := f.Handler.Delete(ctx, uri); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if err := f.Handler.Delete(ctx, uri); err != nil {
		t.Errorf("Delete of a missing object must not be an error: %v", err)
	}
	if _, err := f.Handler.Stat(ctx, uri); !errors.Is(err, remoteio.ErrNotExist) {
		t.Errorf("Stat after delete does not return ErrNotExist: %v", err)
	}
}

// Checks that pseudo-directories arrive as IsPrefix.
// If this were flattened into strings, callers would be back to re-deciding by the trailing delimiter.
func testListShallow(t *testing.T, f Fixture) {
	f.write(t, "data/a.txt", "a")
	f.write(t, "data/sub/b.txt", "b")

	entries := f.collect(t, f.uri("data"), remoteio.ListOptions{Delimiter: "/"})
	got := names(entries)
	if len(got) != 2 {
		t.Fatalf("there should be 2 direct children: got %v", got)
	}

	var sawFile, sawPrefix bool
	for _, e := range entries {
		switch e.Name {
		case "a.txt":
			sawFile = true
			if e.IsPrefix {
				t.Errorf("an object is marked IsPrefix: %+v", e)
			}
		case "sub/":
			sawPrefix = true
			if !e.IsPrefix {
				t.Errorf("a pseudo-directory is not marked IsPrefix: %+v", e)
			}
		default:
			t.Errorf("unexpected Entry: %+v", e)
		}
	}
	if !sawFile || !sawPrefix {
		t.Errorf("both the direct child object and the pseudo-directory are required: got %v", got)
	}
}

func testListRecursive(t *testing.T, f Fixture) {
	f.write(t, "data/a.txt", "a")
	f.write(t, "data/sub/b.txt", "b")

	entries := f.collect(t, f.uri("data"), remoteio.ListOptions{})
	got := names(entries)
	if len(got) != 2 {
		t.Fatalf("everything below must be returned recursively: got %v", got)
	}
	for _, e := range entries {
		if e.IsPrefix {
			t.Errorf("a listing without a delimiter contains a pseudo-directory: %+v", e)
		}
	}
	// Name is relative to the listed prefix.
	if !slices.Contains(got, "a.txt") || !slices.Contains(got, "sub/b.txt") {
		t.Errorf("Name must be relative to the prefix: got %v", got)
	}
}

// Without normalization it is a bare prefix match, and "data" also matches "data-archive/".
// Implementations must always normalize, so the meaning does not change per scheme.
func testListNormalizesPrefix(t *testing.T, f Fixture) {
	f.write(t, "data/a.txt", "a")
	f.write(t, "data-archive/c.txt", "c")

	for _, opts := range []remoteio.ListOptions{{}, {Delimiter: "/"}} {
		for _, e := range f.collect(t, f.uri("data"), opts) {
			if strings.Contains(e.URI, "data-archive") {
				t.Errorf("an adjacent name was picked up (delimiter=%q): %+v", opts.Delimiter, e)
			}
		}
	}
}

// Remote storage has no state such as "a prefix that does not exist".
// Making it an error for local alone would make the same call mean different things per scheme.
func testListEmptyPrefix(t *testing.T, f Fixture) {
	for entry, err := range f.Handler.List(context.Background(), f.uri("nothing-here"), remoteio.ListOptions{}) {
		if err != nil {
			t.Fatalf("listing a prefix with nothing under it must not be an error: %v", err)
		}
		t.Errorf("an Entry came back from a prefix that should be empty: %+v", entry)
	}
}

func testListStops(t *testing.T, f Fixture) {
	f.write(t, "data/a.txt", "a")
	f.write(t, "data/b.txt", "b")
	f.write(t, "data/c.txt", "c")

	var seen int
	for _, err := range f.Handler.List(context.Background(), f.uri("data"), remoteio.ListOptions{}) {
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		seen++
		break
	}
	if seen != 1 {
		t.Errorf("break did not stop the listing: %d entries were processed", seen)
	}
}

// Entry.URI must be complete, so it can be passed as is to the root Store.
func testEntryURIIsUsable(t *testing.T, f Fixture) {
	f.write(t, "data/a.txt", "payload")

	entries := f.collect(t, f.uri("data"), remoteio.ListOptions{})
	if len(entries) == 0 {
		t.Fatal("the listing is empty")
	}
	if got := f.read(t, entries[0].URI); got != "payload" {
		t.Errorf("contents read via Entry.URI differ: got %q", got)
	}
}

// failingReader is a reader that fails after part of it has been read.
type failingReader struct{ r io.Reader }

func (f *failingReader) Read(p []byte) (int, error) {
	if n, _ := f.r.Read(p); n > 0 {
		return n, nil
	}
	return 0, errors.New("storetest: read failed")
}

// Checks the contract that "the destination does not change unless the write succeeds".
func testWriteIsAtomic(t *testing.T, f Fixture) {
	ctx := context.Background()

	t.Run("new", func(t *testing.T) {
		uri := f.uri("atomic/new.txt")
		src := &failingReader{r: bytes.NewReader([]byte("partial-data"))}
		if err := f.Handler.Write(ctx, uri, src, remoteio.WriteOptions{}); err == nil {
			t.Fatal("a write that should fail succeeded")
		}
		if _, err := f.Handler.Stat(ctx, uri); !errors.Is(err, remoteio.ErrNotExist) {
			t.Errorf("a failed write left an object behind: %v", err)
		}
	})

	t.Run("overwrite", func(t *testing.T) {
		uri := f.write(t, "atomic/existing.txt", "original")
		src := &failingReader{r: bytes.NewReader([]byte("partial-data"))}
		if err := f.Handler.Write(ctx, uri, src, remoteio.WriteOptions{}); err == nil {
			t.Fatal("a write that should fail succeeded")
		}
		if got := f.read(t, uri); got != "original" {
			t.Errorf("a failed overwrite broke the existing contents: got %q", got)
		}
	})
}

func testWriteIfNotExists(t *testing.T, f Fixture) {
	if !f.SupportsIfNotExists {
		t.Skip("this implementation does not support conditional writes")
	}
	ctx := context.Background()
	uri := f.uri("once.txt")

	opts := remoteio.WriteOptions{ContentType: remoteio.DefaultContentType, IfNotExists: true}
	if err := f.Handler.Write(ctx, uri, strings.NewReader("first"), opts); err != nil {
		t.Fatalf("first write failed: %v", err)
	}
	if err := f.Handler.Write(ctx, uri, strings.NewReader("second"), opts); !errors.Is(err, remoteio.ErrExist) {
		t.Fatalf("a conditional write over an existing object does not return ErrExist: %v", err)
	}
	if got := f.read(t, uri); got != "first" {
		t.Errorf("the existing contents were broken: got %q", got)
	}
}

// Handlers are exported, so they can be called directly without going through Router.
// Silently processing a foreign scheme would touch a bucket on another cloud.
func testRejectsForeignScheme(t *testing.T, f Fixture) {
	if f.Handler.Scheme() == "" {
		t.Skip("the fallback handler has no scheme")
	}
	foreign := "definitely-not-" + f.Handler.Scheme() + "://bucket/key"
	if _, err := f.Handler.Open(context.Background(), foreign); !errors.Is(err, remoteio.ErrInvalidURI) {
		t.Errorf("a foreign scheme is not rejected with ErrInvalidURI: %v", err)
	}
}

// Prevents confusing it with a bucket operation, and being unable to tell "not there" from "invalid URI".
func testRejectsEmptyObjectName(t *testing.T, f Fixture) {
	if !f.BucketScoped {
		t.Skip("this implementation has no notion of a bucket")
	}
	_, bucket, _, err := remoteio.ParseURI(f.Root)
	if err != nil {
		t.Fatalf("Fixture.Root cannot be parsed as a URI (%s): %v", f.Root, err)
	}

	bucketOnly := remoteio.BuildURI(f.Handler.Scheme(), bucket, "")
	if _, err := f.Handler.Open(context.Background(), bucketOnly); !errors.Is(err, remoteio.ErrInvalidURI) {
		t.Errorf("a URI with an empty object name is not rejected: %v", err)
	}
}
