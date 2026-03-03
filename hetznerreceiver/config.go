package hetznerreceiver

import (
	"fmt"
	"time"
)

// Config defines configuration for the Hetzner Cloud receiver.
type Config struct {
	// APIToken is the Hetzner Cloud API token (required).
	APIToken string `mapstructure:"api_token"`

	// CollectionInterval is the interval at which metrics are collected.
	CollectionInterval time.Duration `mapstructure:"collection_interval"`

	// Servers enables collection of server metrics.
	Servers bool `mapstructure:"servers"`

	// LoadBalancers enables collection of load balancer metrics.
	LoadBalancers bool `mapstructure:"load_balancers"`

	// MetricsStep is the metrics resolution in seconds (60-3600).
	MetricsStep int `mapstructure:"metrics_step"`
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if c.APIToken == "" {
		return fmt.Errorf("api_token must be specified")
	}
	if c.CollectionInterval <= 0 {
		return fmt.Errorf("collection_interval must be positive")
	}
	if c.MetricsStep < 60 || c.MetricsStep > 3600 {
		return fmt.Errorf("metrics_step must be between 60 and 3600")
	}
	if !c.Servers && !c.LoadBalancers {
		return fmt.Errorf("at least one of servers or load_balancers must be enabled")
	}
	return nil
}
