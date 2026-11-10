# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## About this repository

`github.com/shouni/go-remote-io` is a library that handles GCS, S3 and the local file system behind a single interface.

### Conventions

* Comments, doc comments, error / log / panic messages and test assertion messages are in **English** (moved from Japanese on 2026-11-10; do not mix Japanese back in). Error strings follow Go style: lowercase, no trailing punctuation (ST1005).
* Sentinel errors carry the `remoteio: ` prefix. A sentinel is an identifier; a wrap adds context for the reader (`wrapf`).
* `README.md` is English, and `README.ja.md` is its Japanese version. Update the two together.
* Subtest names (`t.Run` names and table `name` fields) are English too.

### Handling breaking changes

**A full rebuild starting from v1.10.1 (formerly planned as v2) is currently being released under v1 tags.** On the premise that there is one user and that breaking past GCS artifacts is acceptable, we chose a flag-day approach that does not use a `/v2` module path. The CI of the 9 dependent repositories goes red at the same time.

* The module path stays `github.com/shouni/go-remote-io` and the package stays `remoteio/`. **Not a single import line changes on the consumer side** (only how the API is used changes).
* Until this rebuild has reached every repository, there is no need to hold back on changing or removing exported identifiers. Once it has, normal compatibility care resumes.

## Commands

```bash
go build ./...
go vet ./...
test -z "$(gofmt -l .)"        # Same format check as CI
go test -race ./...            # CI runs with the race detector on
golangci-lint run              # v2 config (.golangci.yml), CI uses v2.12.2
govulncheck ./...
```

`golangci-lint` / `govulncheck` are installed locally (`~/go/bin`).

## Architecture

### Store is the entry point, and Handler is the one and only extension point

```go
factory, err := gcs.New(ctx)        // owns the client lifecycle
defer factory.Close()
store, err := factory.Store()       // handles gs:// + local + file://

jobs := store.Sub("gs://bucket/jobs")             // decide the scope once, at assembly time
err = jobs.Write(ctx, id+"/status.json", body, remoteio.WithContentType("application/json"))
```

* **`Store`** is the consumer-facing entry point. It has `Open` / `Stat` / `Exists` / `List` / `Write` / `Delete` / `Copy` / `SignURL` / `Sub`. A function that wants a narrower dependency should take `Reader` (just `Open`) or `Writer` (just `Write`).
* **`Handler`** is the only extension point. Extending to a new storage backend means implementing this one interface. Option resolution happens entirely inside the library, so implementations receive already-resolved `ListOptions` / `WriteOptions`.
  * There used to be two extension points (`Lister` and friends taking variadic options, and `SchemeHandler` taking resolved settings). That forced `NewListSettings` / `ListPrefix` / `NewWriteSettings` to be exported, and the only users of those were test fakes.
  * `Exists` is not on `Handler`. It derives from `Stat` + `errors.Is(err, ErrNotExist)`, which removes one method to implement and the room to get the "absence returns `(false, nil)`" promise wrong.
* **`Copier` / `Signer`** are optional interfaces, implemented only by handlers that can. `Store.Copy` falls through to a server-side copy only when src and dst resolve to the same scheme and that handler implements `Copier`; otherwise it relays a stream. **The caller needs no branching.**
  * When `CopyTo` returns `ErrNotSupported`, it also falls back to the stream relay. That is how a handler that cannot decide its support at construction (`Lazy`) reports it at run time.
  * **A copy given a `WriteOption` always relays a stream.** `CopyTo` takes no options, so falling through to a server-side copy silently dropped them — within one scheme, `WithIfNotExists()` still overwrote an existing object, and `WithContentType` was ignored. This is the fix that leaves the shape of `Copier` unchanged.
  * **Both paths carry over the source's Content-Type and metadata** (`carrySourceAttributes`). A server-side copy kept the original attributes while only the stream relay painted over them with the default `text/plain`, so the same `Copy` gave different results depending on the scheme pair. Carrying them over costs one `Stat` round trip, never touches a value the caller set explicitly, and if `Stat` fails it just gives up carrying them over and continues the copy.
* **`Router`** is what `Store` actually is; it looks at the URI's scheme to pick a handler. The dispatch is at run time because paths come from configuration and user input, so which scheme will be handled cannot be decided statically. "Which schemes are supported", on the other hand, is decided at construction, so it is held as explicit data: the set of handlers. **"Unsupported" is decided in one place only, `Router.resolve`.**
* A handler whose `Scheme()` is empty (`NewLocalHandler`) becomes the fallback and receives paths with no scheme. `NewStore` always registers the local and `file://` handlers alongside the remote ones, so **a Store from the gcs factory can still read local paths**, while `s3://` is rejected clearly as unregistered.
* To put several clouds into one `Store`, just line up each factory's `Handler()` in `NewStore`. This used to need `MultiFactory`, `HandlerProvider` and a signer-only `signerRouter`. Making `Signer` an optional interface brought the dispatch back to one place, `Router`.
* `Router.Schemes()` returns them in lexical order. Left in map iteration order, a public API would return a different result on every call.

### `Sub` — scoped stores

A `Store` returned by `Sub` is pinned to a prefix and accepts **only names relative to it**. Passing an absolute URI with a scheme is `ErrAbsoluteName` (so code that thinks it narrowed its scope cannot write to another bucket). Use the root `Store` to handle absolute URIs.

Names containing `.` / `..` segments are rejected with `ErrInvalidURI` too. The local `Join` is `filepath.Join`, which folds `..` and points outside the scope, so the very "can write somewhere else" that `ErrAbsoluteName` should prevent was happening. Remotely the scope is not escaped, but a literal key `gs://b/jobs/../x.txt` is created, so both are rejected together to keep the same call from meaning different things per scheme.

Before this existed, consumers rewrote a thin method calling `BuildGCSURI(bucket, path)` in every repository, and even that was not enough: `"gs://"` was hard-coded in 24 places across the ecosystem.

`Entry.Name` is relative to the listed prefix, so it can be passed straight to the same scoped store. `Entry.URI` is the full URI, so it can be passed to the root `Store`.

### Write atomicity is part of the contract

`Handler.Write` must guarantee that **the destination does not change unless the write succeeds**. A truncated object must never be left behind by a failure part-way. How each of the three implementations meets it:

* **Local** — writes to a temporary file in the same directory, `Sync`s it, then `Rename`s it. With `WithIfNotExists` it uses `Link` instead of `Rename` (it fails with EEXIST when the target exists, so nothing can slip in between the check and the creation). `CreateTemp` creates with 0600, so before the swap it inherits the existing file's permissions or restores 0644. Permissions are applied to the open descriptor, not to the path.
* **GCS** — when `io.Copy` fails, it **sets an error on the pipe before `Close`**. The order is the point:
  * `storage.Writer.Close()` is the API that **completes** the upload. Calling it as is on the failure path leaves a partial object behind (this was a real bug in the old implementation).
  * `cancel()` alone is not enough. `Write` returns with the upload goroutine still running, and nobody waits for it to finish. On top of that, **the GCS fake completes uploads even with a cancelled ctx**, so aborting via ctx is left to the transport.
  * So `CloseWithError` sets an error on the pipe (the request is then certain to fail), then `Close` is called and `<-donec` waits until the goroutine has been cleaned up.
  * When `io.Copy` wrote not a single byte, the Writer has not been opened. Calling `Close` there runs `openWriter` and creates a zero-byte object, so **it is left alone**.
* **S3** — goes through `UploadObject` in `feature/s3/transfermanager`. Calling `PutObject` directly failed to compute the pre-signing checksum unless the body was an `io.Seeker`, so against non-TLS endpoints (MinIO / R2 / test fakes) nothing could be written at all. The `Uploader` in the old `feature/s3/manager` is deprecated (discussion #3306), so the successor is used. The successor is v0.x and its API may move, but only one place in this ecosystem uses `s3://`, so not carrying a deprecated API was preferred over the cost of keeping up.

### `List` is an iterator, `Entry` is typed

```go
for entry, err := range store.List(ctx, "jobs", remoteio.WithDelimiter("/")) {
    if err != nil { return err }
    if entry.IsPrefix { ... }
}
```

* Pseudo-directories are identified by `Entry.IsPrefix`. The callback used to receive only a string, so callers decided by looking at the trailing delimiter. The cause was that **GCS (`attrs.Prefix`) and S3 (`CommonPrefixes`) had typed information that was flattened into strings**.
* When iteration fails part-way, the error is yielded once with a zero-value `Entry` and iteration stops. `break` stops the iteration as is.
* **The prefix is always normalized by `ListPrefix`** (`data` → `data/`). It used to be normalized only when a delimiter was given; without one, GCS / S3 did a bare prefix match (`data` also matched `data-archive/`) while local walked a directory — a scheme-dependent mismatch.
* A local listing without a delimiter returns only files, recursively, via `filepath.WalkDir`. GCS / S3 return everything under the prefix recursively, so if local alone stopped at the direct children, the same call would mean different things per scheme.
* **Deleting everything under a prefix is `DeletePrefix`.** `Delete` is for one object, and given a prefix it silently succeeds as "not there" (one consumer did exactly that in production, and nothing was deleted). Five consumers hand-wrote "list, then delete one by one", varying in whether they checked the range (empty, trailing `/`) and whether they stopped on failure or kept going with `errors.Join`, so it moved into the library. The name to delete is built with `Join(prefix, entry.Name)` — `Entry.URI` is `ErrAbsoluteName` on a scoped store, and `Name` alone is a relative name on the root store; `TestDeletePrefixRemote` pins both. Only the bucket root (an empty object name) is rejected with `ErrInvalidURI`, and no trailing `/` is required (`List`'s normalization tells `data` and `data-archive/` apart).

### Error conventions

Sentinels are gathered in one place. That callers can branch with `errors.Is` rather than on message strings is the condition for this abstraction to work.

| Sentinel | Meaning |
| :-- | :-- |
| `ErrNotExist` (= `fs.ErrNotExist`) | The target does not exist. Also matches `os.ErrNotExist` |
| `ErrExist` (= `fs.ErrExist`) | With `WithIfNotExists`, the target already existed |
| `ErrUnsupportedScheme` | A scheme no handler is responsible for |
| `ErrClosed` | A closed factory was used |
| `ErrNotSupported` | The handler does not support that optional feature |
| `ErrAbsoluteName` | An absolute URI was passed to a scoped store |
| `ErrInvalidURI` | The URI is not in the expected shape |

* Wrapping goes through `wrapf`, to keep the form "add context while keeping the sentinel" the same across every file.
* **`Exists` looks only at whether an object exists.** Both local directories and remote pseudo-directories are `false`. Local `Stat` and `Open` return `ErrNotExist` for a directory (`os.Open` succeeds on a directory and only fails at read time, so otherwise local alone would have "things that open but cannot be read"). Check whether a hierarchy exists with `List`.
* Per-object operations explicitly reject a URI with an empty object name (`gs://bucket` and the like) (`ParseObjectURI`). This prevents confusing it with a bucket operation, and being unable to tell "not there" from "invalid URI". Only listing uses `ParseBucketURI`, because an empty prefix is meaningful there.
* A handler rejects schemes it is not responsible for. Through `Store` the dispatch filters them out, but handlers are exported and can be called directly (`gcs.Handler.Open(ctx, "s3://b/k")` used to read GCS bucket b).
* S3 typed errors are detected with `errors.AsType[*types.NoSuchKey](err)` (Go 1.26's `errors.AsType`). `HeadObject` also returns `NotFound`, so both are checked. A failed conditional write is detected by `smithy.APIError`'s `PreconditionFailed`.

### Scheme vocabulary

The constants are gathered in one block. **Only names without the separator (`SchemeGCS = "gs"`) are exported**, and `Handler.Scheme()` and `Router`'s registration keys use the same form (following RFC 3986; the prefix form `"gs://"` used to be exported).

Adding the separator is confined to the place that defines the scheme vocabulary. Callers never need to concatenate `"://"` themselves:

* `Scheme(uri)` — extracts the scheme name from a URI (empty for a local path)
* `HasScheme(uri, scheme)` — the check. A bare `strings.HasPrefix` would also catch `"gsfoo://..."`
* `BuildURI(scheme, bucket, object)` / `ParseURI(uri)`

### URI and path utilities

Path operations (`Dir` / `Join`) do not go through `net/url`. `gs://` / `s3://` are not URLs but "scheme + bucket + raw key", and rebuilding a space or `?` in an object name as a URL turns it into `%20` or a query separator. `ParseURI` does not decode, so a mangled URI points at a different key without an error. For the same reason `filepath.Dir` is not used for remote URIs either (separators get mixed on Windows).

### No public API without users

Following `Lazy`, we removed `FS` (an `io/fs.FS` adapter, 217 lines), `Move` (`Copy` + `Delete`) and `IsRemote` (`Scheme` is enough). None of them was called anywhere in the fleet; being public, they had to be maintained as a contract, while there was no chance to learn from how they were actually used. When one is needed, add it then, once the shape of the real demand is known.

`io/fs` will not become a core abstraction either, because `fs.FS.Open` takes no context, and an interface that cannot pass cancellation and deadlines to network I/O cannot be the foundation (`FS` got by holding the ctx in a struct).

`SchemeFile` stays even with zero users. It is one vocabulary with `SchemeGCS` / `SchemeS3`, and unexporting it alone would make code handling `file://` hard-code `"file"`.

### Package responsibilities

| Package | Holds |
| :-- | :-- |
| `remoteio` | Abstractions (`Store` / `Handler` / `Entry` / error vocabulary / URI and path operations) and the local and `file://` implementation |
| `remoteio/gcs`, `remoteio/s3` | The only places that touch a cloud SDK. Client lifetime (`Option`) and the `Handler` / `Copier` / `Signer` implementations |
| `remoteio/memio` | In-memory `Handler`, used in tests in place of the real ones |
| `remoteio/storetest` | Conformance test suite for `Handler` |

The `remoteio` package **does not import any cloud SDK**. This boundary keeps cloud SDKs out of the build of applications that use only the abstractions, so do not add SDK dependencies directly under `remoteio`.

In the scheme packages, split URIs through `parseObjectURI` (object name required) or `parseBucketURI` (for listings and other cases where the prefix may be empty). Both include the client nil check, and beyond that they are the shared functions on the `remoteio` side.

### Factories

* `Factory` requires `Close` / `Store()` / `Handler()`. Because `Handler()` exists, no separate interface is needed to bundle several clouds.
* **The library does not hold lazy construction.** We once provided `Lazy(scheme, open)`, but what consumers actually wanted to cache was not the `Handler` but the **lifetime** of the `Factory` (`Close` and refusing use after release). A `Handler` has no lifetime, so `Lazy` was not enough, and it only added public API with zero users, so it was removed. When it is needed, add it then, once the shape of the real demand is known.
* Construction is swapped via `Option` (`gcs.WithClient` / `s3.WithEndpoint` and so on). The lifecycle of a client injected with `WithClient` stays with the caller, and `Close` does not close it (GCS tracks this with `ownsClient`).
* **`Close` and each accessor are safe to call concurrently** (`sync.RWMutex`). The client field used to be set to nil without synchronization. CI runs with `-race`, but no test touched it concurrently, so it went undetected. `Close` is idempotent, and accessors afterwards return `ErrClosed`.
* When no S3 region is configured, the default is `ap-northeast-1` (`s3.DefaultRegion`). Precedence: `WithRegion` > environment or config file > default.
* Signed URLs are strict per scheme. S3 supports only GET / PUT and returns `ErrNotSupported` for anything else.

## Tests

* testify (`require` for preconditions, `assert` for checks) + `t.Run` subtests. Table-driven by default.
* Reading, writing, listing and existence checks on remote paths actually run against **in-process fakes** (`remoteio/gcs/handler_integration_test.go` uses `fsouza/fake-gcs-server`, `remoteio/s3/handler_integration_test.go` uses `johannesboyne/gofakes3`). Both start in-process as Go libraries, so neither docker nor credentials are needed.
* **The fakes sometimes differ from the real thing.** The GCS fake completes uploads even with a cancelled ctx, and gofakes3 is plain HTTP, so the non-Seeker body condition differs from production. Both differences exposed weaknesses in the implementation, so fix the implementation rather than working around the fake. When verifying a feature the fake does not interpret, do not let it pass silently: leave the reason in a `t.Skip`.
* `remoteio/multi_integration_test.go` is `package remoteio_test` (an external test package) and verifies the GCS and S3 fakes bundled into one `Store`. **Cross-cloud copies are exercised only here** (the source stream is not an `io.Seeker`, so regressions in the S3 write implementation show up here). Being an external test package, it does not bring cloud SDKs into the dependencies of `remoteio` itself.
* The `io/fs` adapter was run through `fstest.TestFS`. That avoided rewriting conventions such as name validation, `ReadDir` ordering and `Stat`/`Open` consistency as our own assertions, and it actually found 3 inconsistencies in directory handling.
* Each factory has a test calling `Close` and each accessor concurrently (`TestFactoryCloseIsRaceFree`). Without it, `-race` cannot detect the race.
* Interface satisfaction is guaranteed by compile-time checks in package variables (`var _ remoteio.Copier = (*gcs.Handler)(nil)` and so on). If a `Copier` implementation comes off, `Copy` silently falls back to the stream relay and just gets slower, without anyone noticing.

## Commits / branches

Conventional Commits (`feat(remoteio):`, `fix:`, `chore(deps):`, `test:`, `refactor:`, `docs:`). CI runs on pushes and PRs to `main` and `develop`. The working branch is `develop`, and PRs normally target `main`.
