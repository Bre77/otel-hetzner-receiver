package hetznerreceiver

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *Config
		expectedErr string
	}{
		{
			name: "valid config with defaults",
			cfg: &Config{
				APIToken:           "test-token",
				CollectionInterval: 60 * time.Second,
				Servers:            true,
				LoadBalancers:      true,
				MetricsStep:        60,
			},
		},
		{
			name: "valid config servers only",
			cfg: &Config{
				APIToken:           "test-token",
				CollectionInterval: 30 * time.Second,
				Servers:            true,
				LoadBalancers:      false,
				MetricsStep:        300,
			},
		},
		{
			name: "valid config load balancers only",
			cfg: &Config{
				APIToken:           "test-token",
				CollectionInterval: 60 * time.Second,
				Servers:            false,
				LoadBalancers:      true,
				MetricsStep:        3600,
			},
		},
		{
			name: "missing api_token",
			cfg: &Config{
				APIToken:           "",
				CollectionInterval: 60 * time.Second,
				Servers:            true,
				LoadBalancers:      true,
				MetricsStep:        60,
			},
			expectedErr: "api_token must be specified",
		},
		{
			name: "zero collection interval",
			cfg: &Config{
				APIToken:           "test-token",
				CollectionInterval: 0,
				Servers:            true,
				LoadBalancers:      true,
				MetricsStep:        60,
			},
			expectedErr: "collection_interval must be positive",
		},
		{
			name: "negative collection interval",
			cfg: &Config{
				APIToken:           "test-token",
				CollectionInterval: -1 * time.Second,
				Servers:            true,
				LoadBalancers:      true,
				MetricsStep:        60,
			},
			expectedErr: "collection_interval must be positive",
		},
		{
			name: "metrics_step too low",
			cfg: &Config{
				APIToken:           "test-token",
				CollectionInterval: 60 * time.Second,
				Servers:            true,
				LoadBalancers:      true,
				MetricsStep:        10,
			},
			expectedErr: "metrics_step must be between 60 and 3600",
		},
		{
			name: "metrics_step too high",
			cfg: &Config{
				APIToken:           "test-token",
				CollectionInterval: 60 * time.Second,
				Servers:            true,
				LoadBalancers:      true,
				MetricsStep:        7200,
			},
			expectedErr: "metrics_step must be between 60 and 3600",
		},
		{
			name: "nothing enabled",
			cfg: &Config{
				APIToken:           "test-token",
				CollectionInterval: 60 * time.Second,
				Servers:            false,
				LoadBalancers:      false,
				MetricsStep:        60,
			},
			expectedErr: "at least one of servers or load_balancers must be enabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.expectedErr != "" {
				require.Error(t, err)
				assert.EqualError(t, err, tt.expectedErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
