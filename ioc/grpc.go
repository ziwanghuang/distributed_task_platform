package ioc

import (
	notificationv1 "gitee.com/flycash/notification-platform/api/proto/gen/notification/v1"
	grpcapi "gitee.com/flycash/notification-platform/internal/api/grpc"
	"gitee.com/flycash/notification-platform/internal/api/grpc/interceptor/jwt"
	"gitee.com/flycash/notification-platform/internal/api/grpc/interceptor/log"
	"gitee.com/flycash/notification-platform/internal/api/grpc/interceptor/metrics"
	"gitee.com/flycash/notification-platform/internal/api/grpc/interceptor/tracing"
	"github.com/ego-component/eetcd"
	"github.com/ego-component/eetcd/registry"
	"github.com/gotomicro/ego/client/egrpc/resolver"
	"github.com/gotomicro/ego/core/econf"
	"github.com/gotomicro/ego/server/egrpc"
)

func InitGrpc(noserver *grpcapi.NotificationServer, etcdClient *eetcd.Component) *egrpc.Component {
	// 注册全局的注册中心
	type Config struct {
		Key string `yaml:"key"`
	}
	var cfg Config

	err := econf.UnmarshalKey("jwt", &cfg)
	if err != nil {
		panic("config err:" + err.Error())
	}

	reg := registry.Load("").Build(registry.WithClientEtcd(etcdClient))
	resolver.Register("etcd", reg)

	// 创建observability拦截器
	metricsInterceptor := metrics.New().Build()
	// 创建日志拦截器
	logInterceptor := log.New().Build()

	traceInterceptor := tracing.UnaryServerInterceptor()
	jwtinterceter := jwt.NewJwtAuth(cfg.Key)
	server := egrpc.Load("server.grpc").Build(
		egrpc.WithUnaryInterceptor(metricsInterceptor, logInterceptor, traceInterceptor, jwtinterceter.JwtAuthInterceptor()),
	)

	notificationv1.RegisterNotificationServiceServer(server.Server, noserver)
	notificationv1.RegisterNotificationQueryServiceServer(server.Server, noserver)

	return server
}
