package hetznerreceiver

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

type metricDef struct {
	Name string
	Unit string
}

// serverMetricMap maps simple Hetzner TimeSeries keys to OTel metric names and units.
var serverMetricMap = map[string]metricDef{
	"cpu": {"hetzner.server.cpu", "percent"},
}

// serverDiskMetricMap maps disk metric suffixes (after "disk.N.") to OTel metric definitions.
var serverDiskMetricMap = map[string]metricDef{
	"iops.read":      {"hetzner.server.disk.iops.read", "{operations}/s"},
	"iops.write":     {"hetzner.server.disk.iops.write", "{operations}/s"},
	"bandwidth.read":  {"hetzner.server.disk.bandwidth.read", "By/s"},
	"bandwidth.write": {"hetzner.server.disk.bandwidth.write", "By/s"},
}

// serverNetworkMetricMap maps network metric suffixes (after "network.N.") to OTel metric definitions.
var serverNetworkMetricMap = map[string]metricDef{
	"bandwidth.in":  {"hetzner.server.network.bandwidth.in", "By/s"},
	"bandwidth.out": {"hetzner.server.network.bandwidth.out", "By/s"},
	"pps.in":        {"hetzner.server.network.pps.in", "{packets}/s"},
	"pps.out":       {"hetzner.server.network.pps.out", "{packets}/s"},
}

// lbMetricMap maps Hetzner TimeSeries keys to OTel metric names and units.
var lbMetricMap = map[string]struct {
	Name string
	Unit string
}{
	"open_connections":       {"hetzner.load_balancer.open_connections", "{connections}"},
	"connections_per_second": {"hetzner.load_balancer.connections_per_second", "{connections}/s"},
	"requests_per_second":    {"hetzner.load_balancer.requests_per_second", "{requests}/s"},
	"bandwidth.in":           {"hetzner.load_balancer.bandwidth.in", "By/s"},
	"bandwidth.out":          {"hetzner.load_balancer.bandwidth.out", "By/s"},
}

// addGauge adds a gauge metric with a single data point to the given ScopeMetrics.
func addGauge(sm pmetric.ScopeMetrics, name, unit string, value float64, ts pcommon.Timestamp) {
	m := sm.Metrics().AppendEmpty()
	m.SetName(name)
	m.SetUnit(unit)
	dp := m.SetEmptyGauge().DataPoints().AppendEmpty()
	dp.SetDoubleValue(value)
	dp.SetTimestamp(ts)
}

// addGaugeWithAttr adds a gauge metric with a single data point and one attribute.
func addGaugeWithAttr(sm pmetric.ScopeMetrics, name, unit string, value float64, ts pcommon.Timestamp, attrKey, attrVal string) {
	m := sm.Metrics().AppendEmpty()
	m.SetName(name)
	m.SetUnit(unit)
	dp := m.SetEmptyGauge().DataPoints().AppendEmpty()
	dp.SetDoubleValue(value)
	dp.SetTimestamp(ts)
	dp.Attributes().PutStr(attrKey, attrVal)
}

// parseIndexedMetricKey parses keys like "disk.0.iops.read" or "network.1.bandwidth.in"
// into (resourceType, index, suffix) e.g. ("disk", "0", "iops.read").
func parseIndexedMetricKey(key string) (resourceType, index, suffix string) {
	first, rest, ok := strings.Cut(key, ".")
	if !ok {
		return "", "", ""
	}
	if first != "disk" && first != "network" {
		return "", "", ""
	}
	idx, sfx, ok := strings.Cut(rest, ".")
	if !ok {
		return "", "", ""
	}
	if _, err := strconv.Atoi(idx); err != nil {
		return "", "", ""
	}
	return first, idx, sfx
}

// setServerResourceAttributes sets OTel resource attributes for a Hetzner server.
func setServerResourceAttributes(res pcommon.Resource, server *hcloud.Server) {
	attrs := res.Attributes()

	// OTel semantic conventions
	attrs.PutStr("cloud.provider", "hetzner")
	attrs.PutStr("cloud.platform", "hetzner_cloud")
	attrs.PutStr("host.id", fmt.Sprintf("%d", server.ID))
	attrs.PutStr("host.name", server.Name)
	attrs.PutStr("host.type", server.ServerType.Name)

	if server.PublicNet.IPv4.IP != nil {
		attrs.PutStr("host.ip", server.PublicNet.IPv4.IP.String())
	}

	if server.Datacenter != nil {
		attrs.PutStr("cloud.availability_zone", server.Datacenter.Name)
		if server.Datacenter.Location != nil {
			attrs.PutStr("cloud.region", server.Datacenter.Location.Name)
		}
	}

	// Hetzner-specific
	attrs.PutStr("hetzner.server.status", string(server.Status))
	attrs.PutInt("hetzner.server.type.cores", int64(server.ServerType.Cores))
	attrs.PutDouble("hetzner.server.type.memory", float64(server.ServerType.Memory))
	attrs.PutInt("hetzner.server.type.disk", int64(server.ServerType.Disk))
	attrs.PutStr("hetzner.server.type.cpu_type", string(server.ServerType.CPUType))

	// User labels
	for k, v := range server.Labels {
		attrs.PutStr("hetzner.label."+k, v)
	}
}

// setLBResourceAttributes sets OTel resource attributes for a Hetzner load balancer.
func setLBResourceAttributes(res pcommon.Resource, lb *hcloud.LoadBalancer) {
	attrs := res.Attributes()

	// OTel semantic conventions
	attrs.PutStr("cloud.provider", "hetzner")
	attrs.PutStr("cloud.platform", "hetzner_cloud")

	if lb.Location != nil {
		attrs.PutStr("cloud.region", lb.Location.Name)
	}

	// Hetzner-specific
	attrs.PutStr("hetzner.load_balancer.id", fmt.Sprintf("%d", lb.ID))
	attrs.PutStr("hetzner.load_balancer.name", lb.Name)
	attrs.PutStr("hetzner.load_balancer.type", lb.LoadBalancerType.Name)
	attrs.PutInt("hetzner.load_balancer.type.max_connections", int64(lb.LoadBalancerType.MaxConnections))
	attrs.PutInt("hetzner.load_balancer.type.max_services", int64(lb.LoadBalancerType.MaxServices))
	attrs.PutInt("hetzner.load_balancer.type.max_targets", int64(lb.LoadBalancerType.MaxTargets))
	attrs.PutInt("hetzner.load_balancer.services_count", int64(len(lb.Services)))
	attrs.PutInt("hetzner.load_balancer.targets_count", int64(len(lb.Targets)))

	// User labels
	for k, v := range lb.Labels {
		attrs.PutStr("hetzner.label."+k, v)
	}
}

// lastTimeSeriesValue extracts the last numeric value from a Hetzner TimeSeries.
// Returns 0, false if the series is empty or the value cannot be parsed.
func lastTimeSeriesValue(values []hcloud.ServerMetricsValue) (float64, bool) {
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
