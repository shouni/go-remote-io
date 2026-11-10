# 📁 Go Remote IO

[![CI](https://github.com/shouni/go-remote-io/actions/workflows/ci.yml/badge.svg)](https://github.com/shouni/go-remote-io/actions/workflows/ci.yml)
[![Status](https://img.shields.io/badge/Status-Active-brightgreen)](#)
[![Language](https://img.shields.io/badge/Language-Go-blue)](https://go.dev/)
[![Go Version](https://img.shields.io/github/go-mod/go-version/shouni/go-remote-io)](https://go.dev/)
[![GitHub tag (latest by date)](https://img.shields.io/github/v/tag/shouni/go-remote-io)](https://github.com/shouni/go-remote-io/tags)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Reference](https://pkg.go.dev/badge/github.com/shouni/go-remote-io.svg)](https://pkg.go.dev/github.com/shouni/go-remote-io)

English | [日本語](README.ja.md)

## 🚀 About - read and write GCS, S3 and local files through one Store; what goes there is the caller's call

Go Remote IO is an I/O library that handles **Google Cloud Storage**, **Amazon S3** and the **local
file system** behind a single interface. Whether the name is `gs://`, `s3://`, `file://` or a local
path, one `Open(ctx, name)` reads it, and writing, listing, existence checks, deletion, copying and
signed URLs are all called through the same entry point.

The only thing it takes on is **absorbing the differences between storage backends**. Object naming,
what to store as an artifact and preparing credentials all stay with the caller. It does not
interpret the bytes it hands back either.

### Where go-web-reader draws the line

The sister library [go-web-reader](https://github.com/shouni/go-web-reader) also reads `gs://`, but
it serves a different stage. **go-remote-io is where artifacts are kept**: it reads and writes, has
signed URLs and listings, and handles local files too. **go-web-reader is where source material comes
from**: read-only, including `https://`, and it extracts only the main text from HTML. The
item-by-item comparison is in
[go-web-reader's README](https://github.com/shouni/go-web-reader#go-remote-io-との線引き)
(it lives only in the later of the two, because keeping it in both would always leave one stale).

---

## ✨ Features

* **Dispatch by scheme** — looks at `gs://` / `s3://` / `file://` / anything else as local, and
  delegates to the registered handler. **The supported schemes are fixed at construction**, and an
  unregistered scheme is rejected explicitly with `ErrUnsupportedScheme`. A missing configuration is
  never mistaken for an unsupported one.
* **One entry point (`Store`)** — reading, writing, listing, existence checks, metadata, deletion,
  copying and signed URLs all live on one interface. A function that wants a narrower dependency can
  take `Reader` (just `Open`) or `Writer` (just `Write`).
* **Scoped stores (`Sub`)** — creates a store pinned to a prefix, so the bucket name no longer has to
  be carried into every call.
* **Atomic writes** — unless a write succeeds, the destination does not change. Local files use a
  temporary file + `rename`, GCS aborts the upload, and S3 aborts the multipart upload. Conditional
  writes (`WithIfNotExists`) are available too.
* **Typed listings** — `List` returns `iter.Seq2[Entry, error]`, and pseudo-directories are told apart
  by `Entry.IsPrefix`. A `break` stops the iteration as is.
* **Server-side copy** — `Store.Copy` falls through to a server-side copy when the handler supports it
  within the same scheme, and relays a stream otherwise. The caller needs no branching.
* **Scheme-independent errors** — "not found" is always returned wrapping `ErrNotExist` (the same
  value as `io/fs`), so a single `errors.Is` decides it whatever the backend.
* **A core free of cloud SDKs** — the `remoteio` package itself does not import the GCS or AWS SDK.
* **An in-memory implementation for tests** — `memio` provides a handler that meets the same contract
  as the real ones. No hand-written fakes needed.
* **Swappable endpoints** — supports S3-compatible storage such as MinIO and Cloudflare R2, and reusing
  a client you already built.

Interface definitions, sentinel errors, write and list options, and the `gcs` / `s3` connection
options are on [pkg.go.dev](https://pkg.go.dev/github.com/shouni/go-remote-io/remoteio).

---

## 📦 Package Structure

```text
go-remote-io/
└── remoteio/       # Abstractions (Store / Handler / Entry / error vocabulary / URI and path operations) and the local and file:// implementation
    ├── gcs/        # GCS implementation. Client lifetime and Handler / Copier / Signer
    ├── s3/         # S3 implementation. Same as above
    ├── memio/      # In-memory Handler, used in tests in place of the real ones
    └── storetest/  # Conformance test suite for Handler
```

**Only `gcs` and `s3` may touch a cloud SDK.** This boundary keeps SDKs out of the build of
applications that use only the abstractions, so do not add an SDK dependency directly under
`remoteio`.

To extend to a new storage backend, `Handler` is the only thing to implement. Run the handler you
write through `storetest.TestHandler`. GCS, S3, local, `memio` and third-party implementations are all
checked by the same single suite. Without it, CI stays green even when a fake and the real thing drift
apart.

---

## 🚦 Usage

```go
factory, err := gcs.New(ctx)
if err != nil {
    return err
}
defer func() { _ = factory.Close() }()

store, err := factory.Store()
if err != nil {
    return err
}

// Decide the scope once at assembly time, and the bucket no longer has to be carried around
jobs := store.Sub("gs://my-bucket/jobs")
err = jobs.Write(ctx, jobID+"/status.json", body, remoteio.WithContentType("application/json"))
```

A store returned by `Sub` accepts **only names relative to** its prefix (passing an absolute URI is
`ErrAbsoluteName`). To put several clouds into one store, line up each factory's `Handler()` in
`remoteio.NewStore`. Local paths and `file://` are added automatically.

**The points that cost you when stepped on are written in their godoc too** — listing a prefix with
nothing under it returns empty rather than an error, listing without a delimiter returns everything
below recursively, and remote URIs are not URLs, so use `Dir` / `Join` / `BuildURI` rather than
`net/url`.

---

## 🤝 Dependencies

* [cloud.google.com/go/storage](https://github.com/googleapis/google-cloud-go/tree/main/storage) -
  the official GCS Go client (`remoteio/gcs` only)
* [aws-sdk-go-v2](https://github.com/aws/aws-sdk-go-v2) - AWS SDK for Go v2. Writes go through
  [feature/s3/transfermanager](https://github.com/aws/aws-sdk-go-v2/tree/main/feature/s3/transfermanager)
  (`remoteio/s3` only)

Test-only dependencies are [testify](https://github.com/stretchr/testify), plus
[fake-gcs-server](https://github.com/fsouza/fake-gcs-server) and
[gofakes3](https://github.com/johannesboyne/gofakes3) for integration tests with real I/O. Both start
in-process, so running the tests needs neither docker nor credentials.

---

## 📜 License

This project is released under the [MIT License](https://opensource.org/licenses/MIT).
