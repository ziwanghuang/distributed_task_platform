package main

import (
	"fmt"
	"gitee.com/flycash/distributed_task_platform/example/ioc"
	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/repository/dao"
	"gitee.com/flycash/distributed_task_platform/pkg/sqlx"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

// 这个方法用于初始化任务,需要提前将执行节点，调度节点启动
func TestStart(t *testing.T) {
	db := ioc.InitDB()
	taskDAO := ioc.InitTaskDAO(db)
	// 初始化task
	now := time.Now()
	taskName := fmt.Sprintf("sharding_task_%s", uuid.New().String())
	task := dao.Task{
		Name:            taskName,
		CronExpr:        "0 0 * * * ?", // 每分钟执行一次，简单明确
		Type:            domain.NormalTaskType.String(),
		ExecutionMethod: domain.TaskExecutionMethodRemote.String(),
		GrpcConfig: sqlx.JSONColumn[domain.GrpcConfig]{
			Valid: true,
			Val: domain.GrpcConfig{
				ServiceName: serverName,
			},
		},
		ShardingRule: sqlx.JSONColumn[domain.ShardingRule]{
			Valid: true,
			Val: domain.ShardingRule{
				Type: domain.ShardingRuleTypeRange,
				Params: map[string]string{
					"totalNums": "10",
					"step":      "1000",
				},
			},
		},
		Status:   domain.TaskStatusActive.String(),
		Version:  1,
		NextTime: now.Add(3 * time.Second).UnixMilli(),
	}
	_, err := taskDAO.Create(t.Context(), task)
	require.NoError(t, err)
}
