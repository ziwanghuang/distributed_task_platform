# ============================================================
# 分布式任务调度平台 - 多阶段构建 Dockerfile
# ============================================================
# 构建流程:
#   Stage 1 (builder): 基于 golang:1.24.1-alpine 编译静态二进制
#   Stage 2 (runtime): 基于 alpine:3.20 最小化运行镜像（~10MB）
#
# 构建命令:
#   docker build -t scheduler:latest .
#
# 运行命令:
#   docker run -v ./config/docker-config.yaml:/app/config/config.yaml scheduler:latest
# ============================================================

# ---- Stage 1: 编译阶段 ----
FROM golang:1.24.1-alpine AS builder

# 安装编译所需的系统依赖（CGO 需要 gcc，如果纯 Go 则不需要）
RUN apk add --no-cache git ca-certificates tzdata

# 设置工作目录
WORKDIR /build

# 先拷贝依赖描述文件，利用 Docker layer cache 加速重复构建
# 只要 go.mod/go.sum 没变，依赖层就不会重新下载
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# 拷贝全部源代码
COPY . .

# 编译二进制文件
# CGO_ENABLED=0: 纯静态链接，不依赖 glibc，可在 alpine 上运行
# -trimpath: 移除编译路径信息，减小二进制体积并提高安全性
# -ldflags="-s -w": 去掉符号表和调试信息，进一步减小体积
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /build/scheduler \
    ./cmd/scheduler/main.go

# ---- Stage 2: 运行阶段 ----
FROM alpine:3.20

# 安装运行时必要的依赖
# ca-certificates: HTTPS 请求需要
# tzdata: 时区数据，cron 表达式解析需要
RUN apk add --no-cache ca-certificates tzdata

# 设置时区为亚洲/上海
ENV TZ=Asia/Shanghai

# 创建非 root 用户运行服务（安全最佳实践）
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

# 设置工作目录
WORKDIR /app

# 从 builder 阶段拷贝编译好的二进制文件
COPY --from=builder /build/scheduler /app/scheduler

# 创建配置文件目录（运行时通过 volume 挂载实际配置）
RUN mkdir -p /app/config && chown -R appuser:appgroup /app

# 切换到非 root 用户
USER appuser

# 暴露端口
# 9002: gRPC 服务端口（调度器核心通信）
# 9003: Governor 端口（健康检查 + pprof 诊断）
EXPOSE 9002 9003

# 健康检查：通过 Governor 的 /debug/health 端点
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:9003/debug/health || exit 1

# 启动命令
# --config: 指定配置文件路径，运行时通过 volume 挂载
ENTRYPOINT ["/app/scheduler"]
CMD ["--config=/app/config/config.yaml"]
