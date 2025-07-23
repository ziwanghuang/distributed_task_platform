package scheduleparams

import (
	"context"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/pkg/grpc/registry"
)

type Info struct {
	Rule                  domain.ShardingRule
	ExecutorNodeInstances []registry.ServiceInstance
	TaskExecution         domain.TaskExecution
}

// Builder 是一个通用接口，基于 Info 构建分片调度参数
type Builder interface {
	Build(ctx context.Context, info Info) ([]map[string]string, error)
}
