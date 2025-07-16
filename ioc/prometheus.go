package ioc

import (
	"github.com/gotomicro/ego/core/econf"
	prometheusapi "github.com/prometheus/client_golang/api"
)

func InitPrometheusClient() prometheusapi.Client {
	type Config struct {
		URL string `yaml:"url"`
	}
	var cfg Config
	err := econf.UnmarshalKey("prometheus", &cfg)
	if err != nil {
		panic(err)
	}

	client, err := prometheusapi.NewClient(prometheusapi.Config{
		Address: cfg.URL,
	})
	if err != nil {
		panic(err)
	}
	return client
}
