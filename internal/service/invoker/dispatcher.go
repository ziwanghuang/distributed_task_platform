// Copyright 2023 ecodeclub
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package invoker

import (
	"context"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
)

type Dispatcher struct {
	http  *HTTPInvoker
	grpc  *GRPCInvoker
	local *LocalInvoker
}

func NewDispatcher(
	httpInvoker *HTTPInvoker,
	grpcInvoker *GRPCInvoker,
	local *LocalInvoker,
) *Dispatcher {
	return &Dispatcher{
		http:  httpInvoker,
		grpc:  grpcInvoker,
		local: local,
	}
}

func (r *Dispatcher) Name() string {
	return "dispatcher"
}

func (r *Dispatcher) Run(ctx context.Context, execution domain.TaskExecution) (domain.ExecutionState, error) {
	// 根据配置发送执行请求
	switch {
	case execution.Task.GrpcConfig != nil:
		return r.grpc.Run(ctx, execution)
	case execution.Task.HTTPConfig != nil:
		return r.http.Run(ctx, execution)
	default:
		// 都没有就假定是本地的了
		return r.local.Run(ctx, execution)
	}
}
