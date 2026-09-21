package remoteio

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWrapfKeepsSentinelForNamesWithPercent は、名前に % が入っても番兵が保たれることを
// 検証します。整形済みの文脈を書式として再解釈すると %w の対応がずれ、errors.Is が
// 効かなくなります。
func TestWrapfKeepsSentinelForNamesWithPercent(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"missing-100%.txt", "a%20b.txt", "%w.txt", "%!s.txt"} {
		t.Run(name, func(t *testing.T) {
			err := wrapf(ErrNotExist, "%s を開けません", name)

			require.ErrorIs(t, err, ErrNotExist)
			assert.Equal(t, name+" を開けません: "+ErrNotExist.Error(), err.Error())
		})
	}
}

// TestMissingObjectWithPercentInName は、% を含む名前の不在が「無い」として扱われることを
// 利用者から見える形で確かめます。
func TestMissingObjectWithPercentInName(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := NewStore()
	path := filepath.Join(t.TempDir(), "missing-100%.txt")

	ok, err := store.Exists(ctx, path)
	require.NoError(t, err)
	assert.False(t, ok)

	_, err = store.Open(ctx, path)
	require.ErrorIs(t, err, ErrNotExist)

	_, err = ReadAll(ctx, store, path)
	require.ErrorIs(t, err, ErrNotExist)
}
