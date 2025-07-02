package job

import (
	"context"
	"fmt"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
)

var _ Job = &LocalJob{}

// LocalJob 代表一个正在本地执行的任务实例
type LocalJob struct {
	fns map[string]func(ctx context.Context, task domain.Task) error
}

func (l *LocalJob) Name() string {
	return domain.TaskExecutionMethodLocal.String()
}

func (l *LocalJob) Run(ctx context.Context, task domain.Task) *Chans {
	reportCh := make(chan *domain.Report)
	renewCh := make(chan bool)
	errorCh := make(chan error, 1)

	// 立即返回通道
	chans := &Chans{
		Report: reportCh,
		Renew:  renewCh,
		Error:  errorCh,
	}

	// 在后台执行本地任务
	go func() {
		defer func() {
			close(reportCh)
			close(renewCh)
			close(errorCh)
		}()

		fn, ok := l.fns[task.Name]
		if !ok {
			// 未找到函数，发送错误
			select {
			case errorCh <- fmt.Errorf("未注册方法：%s", task.Name):
			case <-ctx.Done():
			}
			return
		}

		// 执行本地函数
		err := fn(ctx, task)
		// 发送执行结果（可能为nil表示成功）
		select {
		case errorCh <- err:
		case <-ctx.Done():
		}
	}()

	return chans
}
