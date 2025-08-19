package ioc

import (
	"gitee.com/flycash/distributed_task_platform/internal/repository/dao"
	"github.com/ego-component/egorm"
)

func InitTaskDAO(db *egorm.Component) dao.TaskDAO {
	return dao.NewGORMTaskDAO(db)
}
