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
	// 初始化db
	db := ioc.InitDB()
	taskDAO := ioc.InitTaskDAO(db)
	// 初始化task
	now := time.Now()
	taskName := fmt.Sprintf("task_%s", uuid.New().String())
	_, err := taskDAO.Create(t.Context(), dao.Task{
		Name:     taskName,
		CronExpr: "0 0 * * * ?",
		GrpcConfig: sqlx.JSONColumn[domain.GrpcConfig]{
			Valid: true,
			Val: domain.GrpcConfig{
				ServiceName: serverName,
			},
		},
		ScheduleParams: sqlx.JSONColumn[map[string]string]{
			Valid: true,
			Val: map[string]string{
				"start": "0",
				"end":   "10000",
			},
		},
		ExecutionMethod: domain.TaskExecutionMethodRemote.String(),
		Status:          domain.TaskStatusActive.String(),
		Version:         1,
		NextTime:        now.Add(3 * time.Second).UnixMilli(),
	})
	require.NoError(t, err)
}
