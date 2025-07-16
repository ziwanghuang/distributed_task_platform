package ioc

import (
	prometheusapi "github.com/prometheus/client_golang/api"
)

func InitPrometheusClient() prometheusapi.Client {
	client, err := prometheusapi.NewClient(prometheusapi.Config{
		Address: "localhost:9090",
	})
	if err != nil {
		panic(err)
	}
	return client
}
