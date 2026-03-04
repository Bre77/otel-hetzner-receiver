package hetznerreceiver

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap"
)

const scopeName = "github.com/Bre77/otel-hetzner-receiver"
const scopeVersion = "0.1.0"

// hcloudAPI defines the interface for Hetzner Cloud API operations used by the scraper.
type hcloudAPI interface {
	AllServers(ctx context.Context) ([]*hcloud.Server, error)
	GetServerMetrics(ctx context.Context, server *hcloud.Server, opts hcloud.ServerGetMetricsOpts) (*hcloud.ServerMetrics, *hcloud.Response, error)
	AllLoadBalancers(ctx context.Context) ([]*hcloud.LoadBalancer, error)
	GetLBMetrics(ctx context.Context, lb *hcloud.LoadBalancer, opts hcloud.LoadBalancerGetMetricsOpts) (*hcloud.LoadBalancerMetrics, *hcloud.Response, error)
}

// hcloudClient wraps the real hcloud.Client to implement hcloudAPI.
type hcloudClient struct {
	client *hcloud.Client
}

func (c *hcloudClient) AllServers(ctx context.Context) ([]*hcloud.Server, error) {
	return c.client.Server.All(ctx)
}

func (c *hcloudClient) GetServerMetrics(ctx context.Context, server *hcloud.Server, opts hcloud.ServerGetMetricsOpts) (*hcloud.ServerMetrics, *hcloud.Response, error) {
	return c.client.Server.GetMetrics(ctx, server, opts)
}

func (c *hcloudClient) AllLoadBalancers(ctx context.Context) ([]*hcloud.LoadBalancer, error) {
	return c.client.LoadBalancer.All(ctx)
}

func (c *hcloudClient) GetLBMetrics(ctx context.Context, lb *hcloud.LoadBalancer, opts hcloud.LoadBalancerGetMetricsOpts) (*hcloud.LoadBalancerMetrics, *hcloud.Response, error) {
	return c.client.LoadBalancer.GetMetrics(ctx, lb, opts)
}

type hetznerScraper struct {
	cfg    *Config
	logger *zap.Logger
	api    hcloudAPI
}

func (s *hetznerScraper) Start(_ context.Context, _ component.Host) error {
	return nil
}

func (s *hetznerScraper) Shutdown(_ context.Context) error {
	return nil
}

func (s *hetznerScraper) Scrape(ctx context.Context) (pmetric.Metrics, error) {
	md := pmetric.NewMetrics()
	now := time.Now()
	start := now.Add(-time.Duration(s.cfg.MetricsStep) * time.Second)

	var scrapeErrors []error

	if s.cfg.Servers {
		if err := s.scrapeServers(ctx, md, start, now); err != nil {
			scrapeErrors = append(scrapeErrors, err)
		}
	}

	if s.cfg.LoadBalancers {
		if err := s.scrapeLoadBalancers(ctx, md, start, now); err != nil {
			scrapeErrors = append(scrapeErrors, err)
		}
	}

	if len(scrapeErrors) > 0 && md.ResourceMetrics().Len() == 0 {
		return md, fmt.Errorf("scrape failed: %v", scrapeErrors)
	}
	if len(scrapeErrors) > 0 {
		for _, err := range scrapeErrors {
			s.logger.Warn("Partial scrape error", zap.Error(err))
		}
	}

	return md, nil
}

func (s *hetznerScraper) scrapeServers(ctx context.Context, md pmetric.Metrics, start, end time.Time) error {
	servers, err := s.api.AllServers(ctx)
	if err != nil {
		return fmt.Errorf("listing servers: %w", err)
	}

	for _, server := range servers {
		rm := md.ResourceMetrics().AppendEmpty()
		setServerResourceAttributes(rm.Resource(), server)

		sm := rm.ScopeMetrics().AppendEmpty()
		sm.Scope().SetName(scopeName)
		sm.Scope().SetVersion(scopeVersion)

		ts := pcommon.NewTimestampFromTime(end)

		// Always emit running gauge
		running := 0.0
		if server.Status == hcloud.ServerStatusRunning {
			running = 1.0
		}
		addGauge(sm, "hetzner.server.running", "1", running, ts)

		// Always emit traffic counters
		addGauge(sm, "hetzner.server.traffic.included", "By", float64(server.IncludedTraffic), ts)
		addGauge(sm, "hetzner.server.traffic.outgoing", "By", float64(server.OutgoingTraffic), ts)
		addGauge(sm, "hetzner.server.traffic.ingoing", "By", float64(server.IngoingTraffic), ts)

		// Only fetch API metrics for running servers
		if server.Status != hcloud.ServerStatusRunning {
			continue
		}

		metrics, _, err := s.api.GetServerMetrics(ctx, server, hcloud.ServerGetMetricsOpts{
			Types: []hcloud.ServerMetricType{
				hcloud.ServerMetricCPU,
				hcloud.ServerMetricDisk,
				hcloud.ServerMetricNetwork,
			},
			Start: start,
			End:   end,
			Step:  s.cfg.MetricsStep,
		})
		if err != nil {
			s.logger.Warn("Failed to get metrics for server",
				zap.String("server", server.Name),
				zap.Int64("id", server.ID),
				zap.Error(err))
			continue
		}

		for key, values := range metrics.TimeSeries {
			// Try simple metric map first (e.g. "cpu")
			if def, ok := serverMetricMap[key]; ok {
				v, ok := lastTimeSeriesValue(values)
				if !ok {
					continue
				}
				addGauge(sm, def.Name, def.Unit, v, ts)
				continue
			}

			// Try dynamic disk/network parsing (e.g. "disk.0.iops.read", "network.1.bandwidth.in")
			resourceType, index, suffix := parseIndexedMetricKey(key)
			if resourceType == "" {
				continue
			}

			var def metricDef
			var attrKey string
			switch resourceType {
			case "disk":
				d, ok := serverDiskMetricMap[suffix]
				if !ok {
					continue
				}
				def = d
				attrKey = "disk_index"
			case "network":
				d, ok := serverNetworkMetricMap[suffix]
				if !ok {
					continue
				}
				def = d
				attrKey = "network_index"
			default:
				continue
			}

			v, ok := lastTimeSeriesValue(values)
			if !ok {
				continue
			}
			addGaugeWithAttr(sm, def.Name, def.Unit, v, ts, attrKey, index)
		}
	}

	return nil
}

func (s *hetznerScraper) scrapeLoadBalancers(ctx context.Context, md pmetric.Metrics, start, end time.Time) error {
	lbs, err := s.api.AllLoadBalancers(ctx)
	if err != nil {
		return fmt.Errorf("listing load balancers: %w", err)
	}

	for _, lb := range lbs {
		rm := md.ResourceMetrics().AppendEmpty()
		setLBResourceAttributes(rm.Resource(), lb)

		sm := rm.ScopeMetrics().AppendEmpty()
		sm.Scope().SetName(scopeName)
		sm.Scope().SetVersion(scopeVersion)

		ts := pcommon.NewTimestampFromTime(end)

		// Traffic counters
		addGauge(sm, "hetzner.load_balancer.traffic.included", "By", float64(lb.IncludedTraffic), ts)
		addGauge(sm, "hetzner.load_balancer.traffic.outgoing", "By", float64(lb.OutgoingTraffic), ts)
		addGauge(sm, "hetzner.load_balancer.traffic.ingoing", "By", float64(lb.IngoingTraffic), ts)

		// Target health
		var healthy, unhealthy int64
		for _, target := range lb.Targets {
			targetHealthy := len(target.HealthStatus) > 0
			for _, hs := range target.HealthStatus {
				if hs.Status != hcloud.LoadBalancerTargetHealthStatusStatusHealthy {
					targetHealthy = false
					break
				}
			}
			if targetHealthy {
				healthy++
			} else {
				unhealthy++
			}
		}
		addGauge(sm, "hetzner.load_balancer.targets.healthy", "{targets}", float64(healthy), ts)
		addGauge(sm, "hetzner.load_balancer.targets.unhealthy", "{targets}", float64(unhealthy), ts)

		// API metrics
		metrics, _, err := s.api.GetLBMetrics(ctx, lb, hcloud.LoadBalancerGetMetricsOpts{
			Types: []hcloud.LoadBalancerMetricType{
				hcloud.LoadBalancerMetricOpenConnections,
				hcloud.LoadBalancerMetricConnectionsPerSecond,
				hcloud.LoadBalancerMetricRequestsPerSecond,
				hcloud.LoadBalancerMetricBandwidth,
			},
			Start: start,
			End:   end,
			Step:  s.cfg.MetricsStep,
		})
		if err != nil {
			s.logger.Warn("Failed to get metrics for load balancer",
				zap.String("lb", lb.Name),
				zap.Int64("id", lb.ID),
				zap.Error(err))
			continue
		}

		for key, values := range metrics.TimeSeries {
			def, ok := lbMetricMap[key]
			if !ok {
				continue
			}
			v, ok := lastLBTimeSeriesValue(values)
			if !ok {
				continue
			}
			addGauge(sm, def.Name, def.Unit, v, ts)
		}
	}

	return nil
}

// lastLBTimeSeriesValue extracts the last numeric value from a LoadBalancer TimeSeries.
func lastLBTimeSeriesValue(values []hcloud.LoadBalancerMetricsValue) (float64, bool) {
	if len(values) == 0 {
		return 0, false
	}
	last := values[len(values)-1]
	v, err := strconv.ParseFloat(last.Value, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
