package gcs

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientFactory_New(t *testing.T) {
	f, err := New(context.Background())
	require.NoError(t, err)
	require.NotNil(t, f)
	require.NotNil(t, f.client)
}

func TestClientFactory_Accessors(t *testing.T) {
	f, err := New(context.Background())
	require.NoError(t, err)

	t.Run("Reader delegates to InputReader", func(t *testing.T) {
		r, err := f.Reader()
		require.NoError(t, err)
		assert.NotNil(t, r)
	})

	t.Run("InputReader succeeds while client is alive", func(t *testing.T) {
		r, err := f.InputReader()
		require.NoError(t, err)
		assert.NotNil(t, r)
	})

	t.Run("Writer delegates to OutputWriter", func(t *testing.T) {
		w, err := f.Writer()
		require.NoError(t, err)
		assert.NotNil(t, w)
	})

	t.Run("OutputWriter succeeds while client is alive", func(t *testing.T) {
		w, err := f.OutputWriter()
		require.NoError(t, err)
		assert.NotNil(t, w)
	})

	t.Run("URLSigner succeeds while client is alive", func(t *testing.T) {
		s, err := f.URLSigner()
		require.NoError(t, err)
		assert.NotNil(t, s)
	})
}

func TestClientFactory_Close(t *testing.T) {
	f, err := New(context.Background())
	require.NoError(t, err)

	require.NoError(t, f.Close())
	assert.Nil(t, f.client)

	t.Run("Close is idempotent", func(t *testing.T) {
		assert.NoError(t, f.Close())
	})

	t.Run("accessors fail after Close", func(t *testing.T) {
		_, err := f.InputReader()
		assert.Error(t, err)

		_, err = f.OutputWriter()
		assert.Error(t, err)

		_, err = f.URLSigner()
		assert.Error(t, err)
	})
}
