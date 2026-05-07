package batcher

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yld/url-shortener/services/shortener/internal/repo"
)

type fakeBatchInserter struct {
	mu      sync.Mutex
	batches [][]repo.ShortURL
	err     error
	rowErr  map[string]error
}

func (f *fakeBatchInserter) InsertBatch(_ context.Context, rows []repo.ShortURL) (repo.BatchResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return repo.BatchResult{}, f.err
	}
	cp := make([]repo.ShortURL, len(rows))
	copy(cp, rows)
	f.batches = append(f.batches, cp)

	out := make([]error, len(rows))
	for i, r := range rows {
		out[i] = f.rowErr[r.Code]
	}
	return repo.BatchResult{Errors: out}, nil
}

func (f *fakeBatchInserter) snapshotBatches() [][]repo.ShortURL {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]repo.ShortURL, len(f.batches))
	for i, b := range f.batches {
		out[i] = append([]repo.ShortURL(nil), b...)
	}
	return out
}

func TestBatcher_FlushesBySize(t *testing.T) {
	t.Parallel()
	f := &fakeBatchInserter{}
	b := New(f, 5, 500*time.Millisecond)
	defer b.Close()

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := b.Insert(context.Background(), repo.ShortURL{Code: codeOf(i)})
			require.NoError(t, err)
		}(i)
	}
	wg.Wait()

	batches := f.snapshotBatches()
	require.Len(t, batches, 1, "all 5 should land in one batch")
	assert.Len(t, batches[0], 5)
}

func TestBatcher_FlushesByTimeout(t *testing.T) {
	t.Parallel()
	f := &fakeBatchInserter{}
	b := New(f, 100, 30*time.Millisecond)
	defer b.Close()

	start := time.Now()
	err := b.Insert(context.Background(), repo.ShortURL{Code: "solo"})
	elapsed := time.Since(start)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, elapsed, 25*time.Millisecond,
		"solo Insert should wait at least the batch timeout")

	batches := f.snapshotBatches()
	require.Len(t, batches, 1)
	assert.Len(t, batches[0], 1)
}

func TestBatcher_PreservesPerRowErrors(t *testing.T) {
	t.Parallel()
	f := &fakeBatchInserter{rowErr: map[string]error{"bad": repo.ErrDuplicateCode}}
	b := New(f, 3, 500*time.Millisecond)
	defer b.Close()

	results := make([]error, 3)
	var wg sync.WaitGroup
	for i, code := range []string{"ok1", "bad", "ok2"} {
		wg.Add(1)
		go func(i int, code string) {
			defer wg.Done()
			results[i] = b.Insert(context.Background(), repo.ShortURL{Code: code})
		}(i, code)
	}
	wg.Wait()

	assert.NoError(t, results[0])
	assert.ErrorIs(t, results[1], repo.ErrDuplicateCode)
	assert.NoError(t, results[2])
}

func TestBatcher_TransactionFailureFailsAll(t *testing.T) {
	t.Parallel()
	f := &fakeBatchInserter{err: errors.New("commit boom")}
	b := New(f, 2, 500*time.Millisecond)
	defer b.Close()

	results := make([]error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = b.Insert(context.Background(), repo.ShortURL{Code: codeOf(i)})
		}(i)
	}
	wg.Wait()

	for _, err := range results {
		assert.EqualError(t, err, "commit boom")
	}
}

func codeOf(i int) string {
	return string(rune('a' + i))
}
