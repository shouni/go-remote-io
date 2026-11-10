package remoteio

import (
	"errors"
	"fmt"
	"io/fs"
)

// Sentinel errors returned by this package. They let callers branch with
// errors.Is rather than on message strings.
//
// Without sentinels, a check such as "this URI scheme is not supported" could only
// be written as a string comparison, and consumers would carry the wording
// themselves. Then a message change in the library would break them silently.
//
// Sentinel messages carry the "remoteio: " prefix. Wrapping adds context for the
// reader (a sentinel is an identifier; a wrap is context for whoever reads it).
var (
	// ErrNotExist reports that the target object or file does not exist.
	// Whatever the scheme, errors.Is(err, remoteio.ErrNotExist) detects it.
	// It is the same value as in io/fs, so it also matches os.ErrNotExist.
	ErrNotExist = fs.ErrNotExist

	// ErrExist reports that, in a write with WithIfNotExists, the target already existed.
	// It is the same value as in io/fs, so it also matches os.ErrExist.
	ErrExist = fs.ErrExist

	// ErrUnsupportedScheme reports a URI scheme that no handler is responsible for.
	ErrUnsupportedScheme = errors.New("remoteio: unsupported URI scheme")

	// ErrClosed reports that a factory or store that was already closed was used.
	ErrClosed = errors.New("remoteio: factory is closed")

	// ErrNotSupported reports that the handler does not support the operation.
	// It is returned when a handler that does not implement an optional interface,
	// such as signed URLs or server-side copy, is asked for it.
	ErrNotSupported = errors.New("remoteio: operation not supported by handler")

	// ErrAbsoluteName reports that an absolute URI with a scheme was passed to a
	// scoped store (the return value of Store.Sub).
	//
	// Letting it through silently would let code that thinks it narrowed its scope
	// write to another bucket. Use the root store to handle absolute URIs.
	ErrAbsoluteName = errors.New("remoteio: absolute URI passed to a scoped store")

	// ErrInvalidURI reports that a URI is not in the expected shape
	// (no scheme, an empty bucket name, an empty object name where one is required, and so on).
	ErrInvalidURI = errors.New("remoteio: invalid URI")
)

// wrapf wraps an error with context while keeping the sentinel.
// It keeps the split — callers decide with errors.Is, readers get an explanation —
// in the same shape across every file.
//
// The context is fully formatted first and then passed as %s. Letting an already
// formatted string be interpreted as a format again would shift the %w pairing as
// soon as an object name contains %, and errors.Is would stop working (object names
// are arbitrary byte sequences).
func wrapf(err error, format string, args ...any) error {
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), err)
}
