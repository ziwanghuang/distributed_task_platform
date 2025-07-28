package loopjob

import (
	"context"
	"sync"

	"gitee.com/flycash/distributed_task_platform/internal/errs"
)

// ResourceSemaphore 信号量，控制抢占资源的最大信号量
type ResourceSemaphore interface {
	Acquire(ctx context.Context) error
	Release(ctx context.Context) error
}

type MaxCntResourceSemaphore struct {
	maxCount int
	curCount int
	mu       *sync.RWMutex
}

func (r *MaxCntResourceSemaphore) Acquire(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.curCount >= r.maxCount {
		return errs.ErrExceedLimit
	}
	r.curCount++
	return nil
}

func (r *MaxCntResourceSemaphore) Release(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.curCount--
	return nil
}

func (r *MaxCntResourceSemaphore) UpdateMaxCount(maxCount int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.maxCount = maxCount
}

func NewResourceSemaphore(maxCount int) *MaxCntResourceSemaphore {
	return &MaxCntResourceSemaphore{
		maxCount: maxCount,
		mu:       &sync.RWMutex{},
		curCount: 0,
	}
}
