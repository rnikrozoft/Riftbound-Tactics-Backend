package server

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Cap persistence work across matches without blocking the authoritative tick.
var persistenceSlots = make(chan struct{}, 32)
var persistenceLifecycle struct {
	sync.Mutex
	stopping bool
	workers  sync.WaitGroup
}
var errPersistenceBusy = errors.New("persistence capacity reached; retry")

type taskResult[T any] struct {
	value T
	err   error
}
type asyncTask[T any] struct{ result <-chan taskResult[T] }

func startTask[T any](work func(context.Context) (T, error)) *asyncTask[T] {
	result := make(chan taskResult[T], 1)
	task := &asyncTask[T]{result: result}
	persistenceLifecycle.Lock()
	defer persistenceLifecycle.Unlock()
	if persistenceLifecycle.stopping {
		result <- taskResult[T]{err: errPersistenceBusy}
		return task
	}
	select {
	case persistenceSlots <- struct{}{}:
		persistenceLifecycle.workers.Add(1)
		go func() {
			defer persistenceLifecycle.workers.Done()
			defer func() { <-persistenceSlots }()
			// Match/request contexts can be cancelled before persistence finishes.
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			value, err := work(ctx)
			result <- taskResult[T]{value, err}
		}()
	default:
		result <- taskResult[T]{err: errPersistenceBusy}
	}
	return task
}

func (t *asyncTask[T]) poll() (value T, err error, done bool) {
	select {
	case result := <-t.result:
		return result.value, result.err, true
	default:
		return value, nil, false
	}
}

func stopPersistence(ctx context.Context) {
	persistenceLifecycle.Lock()
	persistenceLifecycle.stopping = true
	persistenceLifecycle.Unlock()
	done := make(chan struct{})
	go func() { persistenceLifecycle.workers.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
}
