package hetznerreceiver

import (
	"context"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/receiver"
)

var (
	typeStr = component.MustNewType("hetzner")
)

func NewFactory() receiver.Factory {
	return receiver.NewFactory(
		typeStr,
		createDefaultConfig,
		receiver.WithMetrics(createMetricsReceiver, component.StabilityLevelDevelopment),
	)
}

func createDefaultConfig() component.Config {
	return &Config{
		CollectionInterval: 60 * time.Second,
		Servers:            true,
		LoadBalancers:      true,
		MetricsStep:        60,
	}
}

func createMetricsReceiver(
	_ context.Context,
	params receiver.Settings,
	cfg component.Config,
	metricsConsumer consumer.Metrics,
) (receiver.Metrics, error) {
	return newHetznerReceiver(params, cfg.(*Config), metricsConsumer), nil
}
