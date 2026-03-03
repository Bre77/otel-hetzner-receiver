package hetznerreceiver

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/receiver/receivertest"
)

func TestNewFactory(t *testing.T) {
	factory := NewFactory()
	require.NotNil(t, factory)
	assert.Equal(t, component.MustNewType("hetzner"), factory.Type())
}

func TestCreateDefaultConfig(t *testing.T) {
	factory := NewFactory()
	cfg := factory.CreateDefaultConfig()

	require.NotNil(t, cfg)
	hetznerConfig, ok := cfg.(*Config)
	require.True(t, ok)

	assert.Equal(t, "", hetznerConfig.APIToken)
	assert.Equal(t, 60*time.Second, hetznerConfig.CollectionInterval)
	assert.True(t, hetznerConfig.Servers)
	assert.True(t, hetznerConfig.LoadBalancers)
	assert.Equal(t, 60, hetznerConfig.MetricsStep)
}

func TestCreateMetricsReceiver(t *testing.T) {
	factory := NewFactory()
	cfg := factory.CreateDefaultConfig().(*Config)
	cfg.APIToken = "test-token"

	receiver, err := factory.CreateMetrics(
		context.Background(),
		receivertest.NewNopSettings(component.MustNewType("hetzner")),
		cfg,
		consumertest.NewNop(),
	)

	require.NoError(t, err)
	require.NotNil(t, receiver)
}

func TestCreateMetricsReceiverValidation(t *testing.T) {
	factory := NewFactory()
	cfg := &Config{
		APIToken:           "", // Invalid - empty token
		CollectionInterval: 60 * time.Second,
		Servers:            true,
		LoadBalancers:      true,
		MetricsStep:        60,
	}

	// Factory creation should succeed
	receiver, err := factory.CreateMetrics(
		context.Background(),
		receivertest.NewNopSettings(component.MustNewType("hetzner")),
		cfg,
		consumertest.NewNop(),
	)

	require.NoError(t, err)
	require.NotNil(t, receiver)

	// But validation should fail
	require.Error(t, cfg.Validate())
}
