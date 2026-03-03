// Package hetznerreceiver implements an OpenTelemetry Collector receiver that
// collects metrics from the Hetzner Cloud API for servers and load balancers.
//
// The receiver uses the hcloud-go SDK to poll metrics from Hetzner Cloud and
// converts them to OpenTelemetry format.
//
// # Configuration
//
// The receiver supports the following configuration options:
//
//   - api_token: Hetzner Cloud API token (required)
//   - collection_interval: How often to collect metrics (default: 60s)
//   - servers: Collect server metrics (default: true)
//   - load_balancers: Collect load balancer metrics (default: true)
//   - metrics_step: Metrics resolution in seconds, 60-3600 (default: 60)
//
// # Usage
//
// To use this receiver, build a custom OpenTelemetry Collector using the
// OpenTelemetry Collector Builder (ocb) with the provided builder-config.yaml,
// or import the receiver directly:
//
//	import "github.com/Bre77/otel-hetzner-receiver/hetznerreceiver"
//
//	factory := hetznerreceiver.NewFactory()
package hetznerreceiver
