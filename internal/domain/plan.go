package domain

import (
	"gitee.com/flycash/distributed_task_platform/internal/dsl/parser"
)

type Plan struct {
	ID        int64
	Name      string
	CronExpr  string
	ExecExpr  string
	Execution TaskExecution // plan的执行状态，有running success failed 其他状态没啥用，重试都委托给每个任务了。
	// ast解析出来的内容
	ExecPlan parser.TaskPlan
	// 调度节点
	ScheduleNodeID string
	// 所有任务
	Steps []*PlanTask
	Root  []*PlanTask

	ScheduleParams map[string]string // 调度参数（如分页偏移量、处理进度等）
	NextTime       int64             // 下次执行时间戳
	Status         TaskStatus
	Version        int64 // 版本号，用于乐观锁
	CTime          int64 // 创建时间戳
	UTime          int64 // 更新时间戳
}

func (p Plan) RootTask() []*PlanTask {
	return p.Root
}

func (p Plan) GetTask(name string) (*PlanTask, bool) {
	for idx := range p.Steps {
		if p.Steps[idx].Name == name {
			return p.Steps[idx], true
		}
	}
	return nil, false
}
