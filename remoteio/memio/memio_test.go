package memio_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-remote-io/remoteio/memio"
)

func uri(name string) string {
	return remoteio.BuildURI(memio.DefaultScheme, "bucket", name)
}

// Seed / URIs / Len are the entry points for consumers' tests to set up preconditions and check results.
func TestSeedAndInspect(t *testing.T) {
	ctx := context.Background()
	h := memio.New()

	require.NoError(t, h.Seed(uri("jobs/j1/status.json"), []byte(`{"state":"queued"}`)))
	require.NoError(t, h.Seed(uri("jobs/j2/status.json"), []byte(`{"state":"done"}`)))

	assert.Equal(t, 2, h.Len())
	assert.Equal(t, []string{uri("jobs/j1/status.json"), uri("jobs/j2/status.json")}, h.URIs(),
		"URIs are stable in lexical order")

	store := remoteio.NewStore(h)
	data, err := remoteio.ReadAll(ctx, store, uri("jobs/j1/status.json"))
	require.NoError(t, err)
	assert.JSONEq(t, `{"state":"queued"}`, string(data))
}

// Checks that stored contents cannot be modified from the caller's side.
// If they were shared, tests would break each other's state.
func TestStoredDataIsCopied(t *testing.T) {
	ctx := context.Background()
	h := memio.New()

	original := []byte("payload")
	require.NoError(t, h.Seed(uri("a.txt"), original))
	original[0] = 'X'

	store := remoteio.NewStore(h)
	data, err := remoteio.ReadAll(ctx, store, uri("a.txt"))
	require.NoError(t, err)
	assert.Equal(t, "payload", string(data), "modifying the slice passed to Seed must not propagate")

	// Modifying what was read does not propagate to the stored contents either.
	data[0] = 'Y'
	again, err := remoteio.ReadAll(ctx, store, uri("a.txt"))
	require.NoError(t, err)
	assert.Equal(t, "payload", string(again))
}

// A hook for exercising paths the happy path never takes (cleanup after a failed write and so on).
func TestWithFailure(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("storage failure")

	h := memio.New(memio.WithFailure(func(op, uri string) error {
		if op == "write" && strings.HasSuffix(uri, "broken.txt") {
			return boom
		}
		return nil
	}))
	store := remoteio.NewStore(h)

	err := remoteio.WriteAll(ctx, store, uri("broken.txt"), []byte("x"))
	assert.ErrorIs(t, err, boom)
	assert.Equal(t, 0, h.Len(), "a failed write is not stored")

	require.NoError(t, remoteio.WriteAll(ctx, store, uri("fine.txt"), []byte("x")))
	assert.Equal(t, 1, h.Len(), "operations not targeted are processed as usual")
}

func TestWithClockAndScheme(t *testing.T) {
	ctx := context.Background()
	fixed := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)

	// Calling itself gs lets consumers' tests write the same URIs as production.
	h := memio.New(memio.WithScheme(remoteio.SchemeGCS), memio.WithClock(func() time.Time { return fixed }))
	store := remoteio.NewStore(h)

	gcsURI := remoteio.BuildURI(remoteio.SchemeGCS, "bucket", "a.txt")
	require.NoError(t, remoteio.WriteAll(ctx, store, gcsURI, []byte("x")))

	info, err := store.Stat(ctx, gcsURI)
	require.NoError(t, err)
	assert.Equal(t, fixed, info.ModTime)
	assert.Equal(t, remoteio.SchemeGCS, h.Scheme())
}

// It implements Copier, so a Copy within one scheme is not a stream relay.
func TestCopyTo(t *testing.T) {
	ctx := context.Background()
	h := memio.New()
	require.NoError(t, h.Seed(uri("src.txt"), []byte("payload")))

	store := remoteio.NewStore(h)
	require.NoError(t, store.Copy(ctx, uri("src.txt"), uri("dst.txt")))

	data, err := remoteio.ReadAll(ctx, store, uri("dst.txt"))
	require.NoError(t, err)
	assert.Equal(t, "payload", string(data))

	err = store.Copy(ctx, uri("missing.txt"), uri("never.txt"))
	assert.ErrorIs(t, err, remoteio.ErrNotExist)
}
