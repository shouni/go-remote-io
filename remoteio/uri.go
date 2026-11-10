package remoteio

import (
	"fmt"
	"path/filepath"
	"strings"
)

// The scheme vocabulary is gathered here.
//
// Only names without the separator are exported. What RFC 3986 calls the scheme is
// "gs", and adding the separator is confined to this place. Callers never need to
// concatenate "://" themselves (making them write the concatenation surfaces a
// forgotten separator as a prefix match that also catches another scheme such as
// "gsfoo://...").
const (
	// SchemeGCS is the scheme name of Google Cloud Storage.
	SchemeGCS = "gs"
	// SchemeS3 is the scheme name of Amazon S3.
	SchemeS3 = "s3"
	// SchemeFile is the scheme name of file://.
	SchemeFile = "file"

	// schemeSeparator separates a URI's scheme from its body.
	schemeSeparator = "://"
)

// Scheme extracts the scheme name ("gs" and so on) from a URI.
// It returns the empty string for a string without a scheme (a local path).
//
// The decision is made only by the presence of "://", so URIs without a double
// slash, such as "mailto:", are treated as local paths.
func Scheme(uri string) string {
	i := strings.Index(uri, schemeSeparator)
	if i <= 0 {
		return ""
	}
	return uri[:i]
}

// HasScheme reports whether uri has the given scheme.
//
// A bare prefix match such as strings.HasPrefix(uri, "gs") also catches
// "gsfoo://...", so go through this function for the check.
func HasScheme(uri, scheme string) bool {
	return Scheme(uri) == scheme
}

// BuildURI builds a URI from a scheme name, a bucket name and an object path.
// Pass scheme as the name without the separator ("gs").
func BuildURI(scheme, bucket, object string) string {
	uri := scheme + schemeSeparator + bucket
	if object = strings.TrimPrefix(object, "/"); object != "" {
		uri += "/" + object
	}
	return uri
}

// ParseURI splits a URI of the form "scheme + bucket + object path", whatever the
// scheme. The object path may be empty
// (gs://bucket returns ("gs", "bucket", "")).
//
// The scheme is not fixed because Handler and Router are built scheme-independent.
// If this hard-coded gs:// and s3://, a third party writing a handler for a new
// scheme would reimplement just the URI splitting, and its reading of "where the
// bucket name begins" would drift from this function.
func ParseURI(uri string) (scheme, bucket, object string, err error) {
	scheme = Scheme(uri)
	if scheme == "" {
		return "", "", "", fmt.Errorf("%w: no scheme (%s)", ErrInvalidURI, uri)
	}

	body := uri[len(scheme)+len(schemeSeparator):]
	bucket, object, _ = strings.Cut(body, "/")
	if bucket == "" {
		return "", "", "", fmt.Errorf("%w: empty bucket name (%s)", ErrInvalidURI, uri)
	}
	return scheme, bucket, object, nil
}

// ParseBucketURI checks that the URI has the given scheme and then returns the
// bucket name and the object path. The path may be empty (for listings).
//
// It keeps a handler implementation from processing a scheme it is not responsible
// for. Through Router such URIs are filtered out at dispatch, but handlers are
// exported and can be called directly.
func ParseBucketURI(scheme, uri string) (bucket, object string, err error) {
	got, bucket, object, err := ParseURI(uri)
	if err != nil {
		return "", "", err
	}
	if got != scheme {
		return "", "", fmt.Errorf("%w: scheme mismatch (want: %s, got: %s)", ErrInvalidURI, scheme, uri)
	}
	return bucket, object, nil
}

// ParseObjectURI does what ParseBucketURI does and also checks that the object name is not empty.
//
// URIs with an empty object name (gs://bucket and the like) are rejected to prevent
// confusing them with bucket operations, and being unable to tell "not there" from
// "invalid URI".
func ParseObjectURI(scheme, uri string) (bucket, object string, err error) {
	bucket, object, err = ParseBucketURI(scheme, uri)
	if err != nil {
		return "", "", err
	}
	if object == "" {
		return "", "", fmt.Errorf("%w: empty object name (%s)", ErrInvalidURI, uri)
	}
	return bucket, object, nil
}

// NormalizeBucketName tidies up variations in a value received as a bucket "name".
// It drops surrounding whitespace, known scheme prefixes and leading/trailing slashes.
//
// It is meant to be applied once to a bucket name read from configuration before it
// is passed to BuildURI or Store.Sub. Passed through as is, it produces URIs like
// `gs://gs://my-bucket//path`, which fail only at write time.
func NormalizeBucketName(bucket string) string {
	bucket = strings.TrimSpace(bucket)
	// Drop them by prefixes derived from the known vocabulary, so as not to add more hard-coded schemes.
	for _, scheme := range []string{SchemeGCS, SchemeS3, SchemeFile} {
		bucket = strings.TrimPrefix(bucket, scheme+schemeSeparator)
	}
	return strings.Trim(bucket, "/")
}

// ListPrefix normalizes a listing prefix to the form that means "the contents of a directory".
// It returns the prefix as is when it already ends with the separator, or when it is the bucket root (gs://bucket).
//
// It always normalizes, because otherwise the same call would mean different things
// per scheme: without normalization, GCS / S3 do a bare prefix match (`data` also
// hits `data-archive/`) while local walks a directory.
// Router applies it before dispatch, but it is exported for calling handlers directly.
func ListPrefix(uri string) string {
	if uri == "" || strings.HasSuffix(uri, "/") {
		return uri
	}
	// gs://bucket points at the bucket root, so as is it already means "the contents".
	if scheme := Scheme(uri); scheme != "" {
		if !strings.Contains(uri[len(scheme)+len(schemeSeparator):], "/") {
			return uri
		}
	}
	return uri + "/"
}

// Dir extracts the parent directory from a path or URI, ending with the separator.
//
// Remote URIs are not handled with net/url because gs:// / s3:// are not URLs. An
// object name is an arbitrary byte sequence, and spaces and ? are valid characters,
// but rebuilt as a URL they turn into %20 and a query separator. ParseURI does not
// decode, so a mangled URI points at a different key without an error.
// For the same reason filepath.Dir is not used for remote URIs either (separators get mixed on Windows).
func Dir(uri string) string {
	if uri == "" {
		return ""
	}

	if scheme := Scheme(uri); scheme != "" {
		body := uri[len(scheme)+len(schemeSeparator):]
		if strings.HasSuffix(body, "/") {
			return uri
		}
		if before, _, ok := strings.CutLast(body, "/"); ok {
			return uri[:len(scheme)+len(schemeSeparator)] + before + "/"
		}
		// A URI without a key, such as gs://bucket. It points at the bucket root.
		return uri + "/"
	}

	baseDir := filepath.Dir(uri)
	if baseDir == "." {
		return "." + string(filepath.Separator)
	}
	if !strings.HasSuffix(baseDir, string(filepath.Separator)) {
		baseDir += string(filepath.Separator)
	}
	return baseDir
}

// Join appends a name to a base path or URI.
//
// With a scheme it joins with "/", otherwise it joins as a local path.
// Neither filepath.Join nor url.JoinPath is used on the remote side: the former folds
// consecutive slashes and turns `gs://bucket/a` into `gs:/bucket/a`, and the latter
// percent-encodes the object name (see the comment on Dir).
func Join(base, name string) string {
	if Scheme(base) == "" && base != "" {
		return filepath.Join(base, name)
	}
	base = strings.TrimSuffix(base, "/")
	if name = strings.TrimPrefix(name, "/"); name == "" {
		return base
	}
	if base == "" {
		return name
	}
	return base + "/" + name
}

// IndexedPath inserts a sequence number before the path's extension.
// Example: "path/to/image.png", 1 -> "path/to/image_1.png"
//
// The extension is what follows the last "." after the last separator (same as
// filepath.Ext). Remote URIs and local paths follow the same rule, so when only the
// directory part has a ".", as in "gs://b/dir.v2/name", there is no extension.
func IndexedPath(base string, index int) (string, error) {
	if index <= 0 {
		return "", fmt.Errorf("%w: index must be an integer of 1 or more (%d)", ErrInvalidURI, index)
	}
	ext := filepath.Ext(base)
	return fmt.Sprintf("%s_%d%s", strings.TrimSuffix(base, ext), index, ext), nil
}
