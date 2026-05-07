// Package batcher coalesces concurrent Insert calls into a single SQLite
// transaction so that one fsync amortises across many writes. The trade is
// per-call latency vs aggregate throughput: low-load callers wait up to
// timeout for their batch to flush, but a busy shortener can hit the SQLite
// commit ceiling much harder.
//
// This is opt-in. The shortener constructs a WriteBatcher only when
// SHORTENER_BATCH_SIZE > 1, and the service is unaware: the batcher
// implements the same Insert(ctx, row) signature as repo.Repository.
package batcher

import (
	"context"
	"sync"
	"time"

	"github.com/yld/url-shortener/services/shortener/internal/repo"
)

// BatchInserter is the subset of *repo.Repository the batcher depends on.
type BatchInserter interface {
	InsertBatch(ctx context.Context, rows []repo.ShortURL) (repo.BatchResult, error)
}

type pending struct {
	row   repo.ShortURL
	reply chan error
}

// WriteBatcher batches concurrent Inserts into single transactions.
type WriteBatcher struct {
	repo    BatchInserter
	in      chan pending
	size    int
	timeout time.Duration

	closeOnce sync.Once
	wg        sync.WaitGroup
}

// New starts a batcher goroutine. size is the max items per flush; timeout is
// the longest a request can sit in the queue before its batch is forced out.
// size <= 1 effectively disables batching but is still safe to use.
func New(r BatchInserter, size int, timeout time.Duration) *WriteBatcher {
	if size < 1 {
		size = 1
	}
	if timeout <= 0 {
		timeout = 10 * time.Millisecond
	}
	b := &WriteBatcher{
		repo:    r,
		in:      make(chan pending, 1024),
		size:    size,
		timeout: timeout,
	}
	b.wg.Add(1)
	go b.loop()
	return b
}

// Insert blocks until this row's batch flushes (or ctx is canceled).
// Must not be called after Close.
func (b *WriteBatcher) Insert(ctx context.Context, row repo.ShortURL) error {
	reply := make(chan error, 1)
	select {
	case b.in <- pending{row: row, reply: reply}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close drains the in-flight queue and stops the loop. The caller must ensure
// no new Insert calls arrive concurrently (typically: shut down upstream first).
func (b *WriteBatcher) Close() {
	b.closeOnce.Do(func() {
		close(b.in)
	})
	b.wg.Wait()
}

func (b *WriteBatcher) loop() {
	defer b.wg.Done()
	for {
		first, ok := <-b.in
		if !ok {
			return
		}
		batch := []pending{first}
		timer := time.NewTimer(b.timeout)
	gather:
		for len(batch) < b.size {
			select {
			case p, more := <-b.in:
				if !more {
					timer.Stop()
					b.flush(batch)
					return
				}
				batch = append(batch, p)
			case <-timer.C:
				break gather
			}
		}
		timer.Stop()
		b.flush(batch)
	}
}

func (b *WriteBatcher) flush(batch []pending) {
	rows := make([]repo.ShortURL, len(batch))
	for i, p := range batch {
		rows[i] = p.row
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := b.repo.InsertBatch(ctx, rows)
	if err != nil {
		for _, p := range batch {
			p.reply <- err
		}
		return
	}
	for i, p := range batch {
		p.reply <- result.Errors[i]
	}
}
