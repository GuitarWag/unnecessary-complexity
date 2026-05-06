package codegen

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGenerator_RejectsInvalidLength(t *testing.T) {
	t.Parallel()

	_, err := NewGenerator(0, nil)
	assert.ErrorIs(t, err, ErrInvalidLength)

	_, err = NewGenerator(-1, nil)
	assert.ErrorIs(t, err, ErrInvalidLength)
}

func TestNewGenerator_DefaultsRand(t *testing.T) {
	t.Parallel()

	g, err := NewGenerator(7, nil)
	require.NoError(t, err)

	code, err := g.Generate()
	require.NoError(t, err)
	assert.Len(t, code, 7)
}

func TestGenerate_ProducesBase62OfRequestedLength(t *testing.T) {
	t.Parallel()

	g, err := NewGenerator(8, nil)
	require.NoError(t, err)

	for range 50 {
		code, err := g.Generate()
		require.NoError(t, err)
		assert.Len(t, code, 8)
		for _, r := range code {
			assert.Truef(t,
				strings.ContainsRune(alphabet, r),
				"character %q not in base62 alphabet", r,
			)
		}
	}
}

func TestGenerate_DeterministicWithFixedReader(t *testing.T) {
	t.Parallel()

	// Identical reader byte streams must produce identical codes.
	a, err := NewGenerator(10, &fixedReader{data: bytesRepeated(0xAB, 64)})
	require.NoError(t, err)
	b, err := NewGenerator(10, &fixedReader{data: bytesRepeated(0xAB, 64)})
	require.NoError(t, err)

	c1, err := a.Generate()
	require.NoError(t, err)
	c2, err := b.Generate()
	require.NoError(t, err)
	assert.Equal(t, c1, c2)
}

func TestGenerate_RandomnessSpread(t *testing.T) {
	t.Parallel()

	g, err := NewGenerator(8, nil)
	require.NoError(t, err)

	seen := map[string]struct{}{}
	for range 200 {
		c, err := g.Generate()
		require.NoError(t, err)
		seen[c] = struct{}{}
	}
	// 200 generations of 8-char base62 should all collide-free in practice.
	assert.GreaterOrEqual(t, len(seen), 199)
}

func TestGenerate_PropagatesReaderError(t *testing.T) {
	t.Parallel()

	want := errors.New("boom")
	g, err := NewGenerator(6, &errReader{err: want})
	require.NoError(t, err)

	_, err = g.Generate()
	assert.ErrorIs(t, err, want)
}

// --- helpers ---

type fixedReader struct {
	data []byte
	pos  int
}

func (r *fixedReader) Read(p []byte) (int, error) {
	n := copy(p, r.data[r.pos:])
	r.pos += n
	if n == 0 {
		return 0, errors.New("fixedReader exhausted")
	}
	return n, nil
}

type errReader struct{ err error }

func (r *errReader) Read([]byte) (int, error) { return 0, r.err }

func bytesRepeated(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}
