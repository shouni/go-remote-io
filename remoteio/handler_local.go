package remoteio

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"iter"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// localHandler is the Handler for the local file system.
// Its Scheme() is empty, so it receives paths that match no scheme.
type localHandler struct{}

var _ Handler = localHandler{}

// NewLocalHandler returns a handler for the local file system.
// Passed to a Router it becomes the fallback (responsible for paths without a scheme).
func NewLocalHandler() Handler { return localHandler{} }

// Scheme returns the empty string, marking this handler as the fallback.
func (localHandler) Scheme() string { return "" }

// Open opens a local file.
// The error for a missing file contains ErrNotExist, so callers can detect it with
// errors.Is just as for remote storage.
//
// A directory returns ErrNotExist, as with Stat. os.Open succeeds on a directory and
// fails only when it is read, so otherwise local alone would have "things that open
// but cannot be read". Remote storage has no counterpart, so the two are aligned at
// open time.
func (localHandler) Open(_ context.Context, path string) (io.ReadCloser, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, wrapf(err, "open local file (%s)", path)
	}

	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, wrapf(err, "stat local file (%s)", path)
	}
	if info.IsDir() {
		_ = file.Close()
		return nil, wrapf(fs.ErrNotExist, "a directory is not an object (%s)", path)
	}
	return file, nil
}

// Stat returns the metadata of a local file.
// The local file system does not keep a Content-Type, so ContentType is empty.
//
// A directory returns ErrNotExist. See the Store.Exists doc for why.
func (localHandler) Stat(_ context.Context, path string) (ObjectInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return ObjectInfo{}, wrapf(err, "stat local file (%s)", path)
	}
	if info.IsDir() {
		return ObjectInfo{}, wrapf(fs.ErrNotExist, "a directory is not an object (%s)", path)
	}
	return ObjectInfo{URI: path, Size: info.Size(), ModTime: info.ModTime()}, nil
}

// List enumerates what is under path.
//
// Without a delimiter it lists only files, recursively. GCS / S3 listings return
// everything under the prefix recursively, so if local alone stopped at the direct
// children, the same call would mean different things per scheme, and the caller
// could not see the difference. With a delimiter it lists only the direct children
// and also returns directories as Entry values with IsPrefix (the equivalent of
// pseudo-directories).
func (localHandler) List(ctx context.Context, path string, opts ListOptions) iter.Seq2[Entry, error] {
	if opts.Delimiter != "" {
		return listLocalShallow(ctx, path, opts.Delimiter)
	}
	return listLocalRecursive(ctx, path)
}

// listLocalShallow lists only the direct children of path.
func listLocalShallow(ctx context.Context, path, delimiter string) iter.Seq2[Entry, error] {
	return func(yield func(Entry, error) bool) {
		entries, err := os.ReadDir(path)
		if err != nil {
			// A missing directory returns empty, as "a prefix with nothing under it".
			// Remote storage has no state such as "a prefix that does not exist", so making it
			// an error here would make the same call mean different things per scheme.
			// Failures other than absence, such as permission errors, are passed on as is.
			if errors.Is(err, fs.ErrNotExist) {
				return
			}
			yield(Entry{}, wrapf(err, "read local directory (%s)", path))
			return
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				yield(Entry{}, err)
				return
			}

			name := entry.Name()
			full := filepath.Join(path, name)
			if entry.IsDir() {
				if !yield(Entry{URI: full + delimiter, Name: name + delimiter, IsPrefix: true}, nil) {
					return
				}
				continue
			}

			// To avoid adding a Stat just for listing, size and time are returned as zero
			// values when they cannot be read. That is more useful than losing the name.
			var size int64
			var modTime time.Time
			if info, err := entry.Info(); err == nil {
				size, modTime = info.Size(), info.ModTime()
			}
			if !yield(Entry{URI: full, Name: name, Size: size, ModTime: modTime}, nil) {
				return
			}
		}
	}
}

// listLocalRecursive lists the files under path recursively (directories themselves are not returned).
func listLocalRecursive(ctx context.Context, root string) iter.Seq2[Entry, error] {
	return func(yield func(Entry, error) bool) {
		// A sentinel that carries the stop into the walk. A dedicated value keeps the
		// error returned by the callback from being confused with a failure of the walk itself.
		stop := errors.New("remoteio: stop walk")

		err := filepath.WalkDir(root, func(entryPath string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}

			name, relErr := filepath.Rel(root, entryPath)
			if relErr != nil {
				// Walking under root, this is never reached, but if it is, give up on the
				// relative name and use the full path.
				name = entryPath
			}

			var size int64
			var modTime time.Time
			if info, infoErr := d.Info(); infoErr == nil {
				size, modTime = info.Size(), info.ModTime()
			}
			if !yield(Entry{URI: entryPath, Name: filepath.ToSlash(name), Size: size, ModTime: modTime}, nil) {
				return stop
			}
			return nil
		})

		switch {
		case err == nil, errors.Is(err, stop):
			return
		case errors.Is(err, fs.ErrNotExist):
			// For the same reason as listLocalShallow, absence is treated as an empty listing.
			return
		case errors.Is(err, fs.ErrPermission):
			yield(Entry{}, wrapf(err, "read local directory (%s)", root))
		default:
			yield(Entry{}, err)
		}
	}
}

// Write writes to the local file system.
//
// It writes to a temporary file in the same directory and then swaps it in, so the
// destination does not change even when it fails part-way. Writing directly to
// os.Create leaves a partial file behind when ctx cancellation or an I/O error exits early.
//
// Note: metadata such as ContentType and ContentDisposition is not stored and is ignored.
func (localHandler) Write(ctx context.Context, path string, src io.Reader, opts WriteOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	slog.DebugContext(ctx, "local write started",
		slog.String("path", path),
		slog.String("content_type", opts.ContentType),
		slog.String("disposition", opts.ContentDisposition),
		slog.String("cache_control", opts.CacheControl),
	)

	outputDir := filepath.Dir(path)
	if outputDir == "" {
		outputDir = "."
	}
	if outputDir != "." {
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			return wrapf(err, "create output directory (%s)", outputDir)
		}
	}

	tmp, err := os.CreateTemp(outputDir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return wrapf(err, "create local file (%s)", path)
	}
	tmpName := tmp.Name()
	// Always removed on the failure path. On success it has been swapped in, and Remove just gets a not-exist error.
	defer func() { _ = os.Remove(tmpName) }()

	// io.Copy into an os.File does not look at ctx, so ctxReader is put in between to notice cancellation.
	if _, err := io.Copy(tmp, &ctxReader{ctx: ctx, r: src}); err != nil {
		_ = tmp.Close()
		return wrapf(err, "write content to local file (%s)", path)
	}

	// Commit the contents before the rename. Without fsync, a crash can leave a file
	// whose name was swapped but whose contents are empty.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return wrapf(err, "sync local file (%s)", path)
	}

	// CreateTemp creates with 0600, so restore permissions close to a normal create.
	// When replacing an existing file, inherit its permissions.
	mode := fs.FileMode(0o644)
	if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() {
		mode = info.Mode().Perm()
	}
	// Apply them to the open descriptor, not to the path (so the swap target cannot be mistaken).
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return wrapf(err, "set permissions on local file (%s)", path)
	}

	if err := tmp.Close(); err != nil {
		return wrapf(err, "close local file (%s)", path)
	}

	if opts.IfNotExists {
		// link fails with EEXIST when the target already exists. Unlike checking with
		// Exists and then renaming, no other process can slip in between the check and the creation.
		if err := os.Link(tmpName, path); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return wrapf(fs.ErrExist, "local file already exists (%s)", path)
			}
			return wrapf(err, "create local file (%s)", path)
		}
		slog.DebugContext(ctx, "local write finished", slog.String("path", path))
		return nil
	}

	if err := os.Rename(tmpName, path); err != nil {
		return wrapf(err, "replace local file (%s)", path)
	}

	slog.DebugContext(ctx, "local write finished", slog.String("path", path))
	return nil
}

// Delete removes a local file. Absence is not an error.
func (localHandler) Delete(_ context.Context, path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return wrapf(err, "delete local file (%s)", path)
	}
	return nil
}
