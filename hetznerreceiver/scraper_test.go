package hetznerreceiver

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap"
)

// mockAPI implements hcloudAPI for testing.
type mockAPI struct {
	servers     []*hcloud.Server
	serverErr   error
	serverMetrics map[int64]*hcloud.ServerMetrics
	serverMetricErr error

	loadBalancers []*hcloud.LoadBalancer
	lbErr         error
	lbMetrics     map[int64]*hcloud.LoadBalancerMetrics
	lbMetricErr   error
}

func (m *mockAPI) AllServers(_ context.Context) ([]*hcloud.Server, error) {
	return m.servers, m.serverErr
}

func (m *mockAPI) GetServerMetrics(_ context.Context, server *hcloud.Server, _ hcloud.ServerGetMetricsOpts) (*hcloud.ServerMetrics, *hcloud.Response, error) {
	if m.serverMetricErr != nil {
		return nil, nil, m.serverMetricErr
	}
	metrics, ok := m.serverMetrics[server.ID]
	if !ok {
		return nil, nil, fmt.Errorf("no metrics for server %d", server.ID)
	}
	return metrics, nil, nil
}

func (m *mockAPI) AllLoadBalancers(_ context.Context) ([]*hcloud.LoadBalancer, error) {
	return m.loadBalancers, m.lbErr
}

func (m *mockAPI) GetLBMetrics(_ context.Context, lb *hcloud.LoadBalancer, _ hcloud.LoadBalancerGetMetricsOpts) (*hcloud.LoadBalancerMetrics, *hcloud.Response, error) {
	if m.lbMetricErr != nil {
		return nil, nil, m.lbMetricErr
	}
	metrics, ok := m.lbMetrics[lb.ID]
	if !ok {
		return nil, nil, fmt.Errorf("no metrics for lb %d", lb.ID)
	}
	return metrics, nil, nil
}

func testServer(id int64, name string, status hcloud.ServerStatus) *hcloud.Server {
	return &hcloud.Server{
		ID:     id,
		Name:   name,
		Status: status,
		ServerType: &hcloud.ServerType{
			Name:    "cx22",
			Cores:   2,
			Memory:  4,
			Disk:    40,
			CPUType: hcloud.CPUTypeShared,
		},
		Datacenter: &hcloud.Datacenter{
			Name: "fsn1-dc14",
			Location: &hcloud.Location{
				Name: "fsn1",
			},
		},
		PublicNet: hcloud.ServerPublicNet{
			IPv4: hcloud.ServerPublicNetIPv4{
				IP: net.ParseIP("1.2.3.4"),
			},
		},
		Labels:          map[string]string{"env": "test"},
		IncludedTraffic: 654321000000,
		OutgoingTraffic: 123456789,
		IngoingTraffic:  987654321,
	}
}

func testLoadBalancer(id int64, name string) *hcloud.LoadBalancer {
	return &hcloud.LoadBalancer{
		ID:   id,
		Name: name,
		LoadBalancerType: &hcloud.LoadBalancerType{
			Name:           "lb11",
			MaxConnections: 10000,
			MaxServices:    5,
			MaxTargets:     25,
		},
		Location: &hcloud.Location{
			Name: "fsn1",
		},
		Services: []hcloud.LoadBalancerService{{}, {}},
		Targets: []hcloud.LoadBalancerTarget{
			{
				HealthStatus: []hcloud.LoadBalancerTargetHealthStatus{
					{ListenPort: 80, Status: hcloud.LoadBalancerTargetHealthStatusStatusHealthy},
					{ListenPort: 443, Status: hcloud.LoadBalancerTargetHealthStatusStatusHealthy},
				},
			},
			{
				HealthStatus: []hcloud.LoadBalancerTargetHealthStatus{
					{ListenPort: 80, Status: hcloud.LoadBalancerTargetHealthStatusStatusHealthy},
					{ListenPort: 443, Status: hcloud.LoadBalancerTargetHealthStatusStatusUnhealthy},
				},
			},
			{
				HealthStatus: []hcloud.LoadBalancerTargetHealthStatus{
					{ListenPort: 80, Status: hcloud.LoadBalancerTargetHealthStatusStatusHealthy},
					{ListenPort: 443, Status: hcloud.LoadBalancerTargetHealthStatusStatusHealthy},
				},
			},
		},
		Labels:          map[string]string{"env": "prod"},
		IncludedTraffic: 500000000000,
		OutgoingTraffic: 111111111,
		IngoingTraffic:  222222222,
	}
}

func TestScrapeServers(t *testing.T) {
	mock := &mockAPI{
		servers: []*hcloud.Server{
			testServer(1, "web-1", hcloud.ServerStatusRunning),
			testServer(2, "web-2", hcloud.ServerStatusOff),
		},
		serverMetrics: map[int64]*hcloud.ServerMetrics{
			1: {
				TimeSeries: map[string][]hcloud.ServerMetricsValue{
					"cpu": {
						{Timestamp: 1000, Value: "10.5"},
						{Timestamp: 1060, Value: "25.3"},
					},
					"disk.0.iops.read": {
						{Timestamp: 1060, Value: "100"},
					},
					"network.0.bandwidth.in": {
						{Timestamp: 1060, Value: "5000"},
					},
				},
			},
		},
	}

	s := &hetznerScraper{
		cfg: &Config{
			Servers:       true,
			LoadBalancers: false,
			MetricsStep:   60,
		},
		logger: zap.NewNop(),
		api:    mock,
	}

	md, err := s.Scrape(context.Background())
	require.NoError(t, err)

	// Both servers emit resource metrics now (running + off)
	require.Equal(t, 2, md.ResourceMetrics().Len())

	// Find the running server's resource metrics
	var runningSM, offSM pmetric.ScopeMetrics
	for i := 0; i < md.ResourceMetrics().Len(); i++ {
		rm := md.ResourceMetrics().At(i)
		attrs := rm.Resource().Attributes()
		name, _ := attrs.Get("host.name")
		if name.Str() == "web-1" {
			runningSM = rm.ScopeMetrics().At(0)
		} else {
			offSM = rm.ScopeMetrics().At(0)
		}
	}

	// Running server: 1 running + 3 traffic + 3 API metrics = 7
	assert.Equal(t, scopeName, runningSM.Scope().Name())
	assert.Equal(t, 7, runningSM.Metrics().Len())

	metricValues := make(map[string]float64)
	for i := 0; i < runningSM.Metrics().Len(); i++ {
		m := runningSM.Metrics().At(i)
		dp := m.Gauge().DataPoints().At(0)
		metricValues[m.Name()] = dp.DoubleValue()
	}

	assert.Equal(t, 1.0, metricValues["hetzner.server.running"])
	assert.Equal(t, 654321000000.0, metricValues["hetzner.server.traffic.included"])
	assert.Equal(t, 123456789.0, metricValues["hetzner.server.traffic.outgoing"])
	assert.Equal(t, 987654321.0, metricValues["hetzner.server.traffic.ingoing"])
	assert.Equal(t, 25.3, metricValues["hetzner.server.cpu"])
	assert.Equal(t, 100.0, metricValues["hetzner.server.disk.iops.read"])
	assert.Equal(t, 5000.0, metricValues["hetzner.server.network.bandwidth.in"])

	// Off server: 1 running + 3 traffic = 4 (no API metrics)
	assert.Equal(t, 4, offSM.Metrics().Len())

	offValues := make(map[string]float64)
	for i := 0; i < offSM.Metrics().Len(); i++ {
		m := offSM.Metrics().At(i)
		dp := m.Gauge().DataPoints().At(0)
		offValues[m.Name()] = dp.DoubleValue()
	}
	assert.Equal(t, 0.0, offValues["hetzner.server.running"])
}

func TestScrapeLoadBalancers(t *testing.T) {
	mock := &mockAPI{
		loadBalancers: []*hcloud.LoadBalancer{
			testLoadBalancer(10, "lb-1"),
		},
		lbMetrics: map[int64]*hcloud.LoadBalancerMetrics{
			10: {
				TimeSeries: map[string][]hcloud.LoadBalancerMetricsValue{
					"open_connections": {
						{Timestamp: 1060, Value: "42"},
					},
					"connections_per_second": {
						{Timestamp: 1060, Value: "15.5"},
					},
					"bandwidth.in": {
						{Timestamp: 1060, Value: "10000"},
					},
				},
			},
		},
	}

	s := &hetznerScraper{
		cfg: &Config{
			Servers:       false,
			LoadBalancers: true,
			MetricsStep:   60,
		},
		logger: zap.NewNop(),
		api:    mock,
	}

	md, err := s.Scrape(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, md.ResourceMetrics().Len())

	rm := md.ResourceMetrics().At(0)
	attrs := rm.Resource().Attributes()

	v, ok := attrs.Get("hetzner.load_balancer.name")
	require.True(t, ok)
	assert.Equal(t, "lb-1", v.Str())

	v, ok = attrs.Get("hetzner.load_balancer.type")
	require.True(t, ok)
	assert.Equal(t, "lb11", v.Str())

	v, ok = attrs.Get("hetzner.load_balancer.services_count")
	require.True(t, ok)
	assert.Equal(t, int64(2), v.Int())

	v, ok = attrs.Get("hetzner.load_balancer.targets_count")
	require.True(t, ok)
	assert.Equal(t, int64(3), v.Int())

	v, ok = attrs.Get("hetzner.label.env")
	require.True(t, ok)
	assert.Equal(t, "prod", v.Str())

	// Check metrics: 3 traffic + 2 health + 3 API = 8
	sm := rm.ScopeMetrics().At(0)
	assert.Equal(t, 8, sm.Metrics().Len())

	metricValues := make(map[string]float64)
	for i := 0; i < sm.Metrics().Len(); i++ {
		m := sm.Metrics().At(i)
		dp := m.Gauge().DataPoints().At(0)
		metricValues[m.Name()] = dp.DoubleValue()
	}

	// Traffic counters
	assert.Equal(t, 500000000000.0, metricValues["hetzner.load_balancer.traffic.included"])
	assert.Equal(t, 111111111.0, metricValues["hetzner.load_balancer.traffic.outgoing"])
	assert.Equal(t, 222222222.0, metricValues["hetzner.load_balancer.traffic.ingoing"])

	// Target health (2 healthy, 1 unhealthy - target[1] has one unhealthy port)
	assert.Equal(t, 2.0, metricValues["hetzner.load_balancer.targets.healthy"])
	assert.Equal(t, 1.0, metricValues["hetzner.load_balancer.targets.unhealthy"])

	// API metrics
	assert.Equal(t, 42.0, metricValues["hetzner.load_balancer.open_connections"])
	assert.Equal(t, 15.5, metricValues["hetzner.load_balancer.connections_per_second"])
	assert.Equal(t, 10000.0, metricValues["hetzner.load_balancer.bandwidth.in"])
}

func TestScrapeServerListError(t *testing.T) {
	mock := &mockAPI{
		serverErr: fmt.Errorf("api error"),
	}

	s := &hetznerScraper{
		cfg: &Config{
			Servers:       true,
			LoadBalancers: false,
			MetricsStep:   60,
		},
		logger: zap.NewNop(),
		api:    mock,
	}

	md, err := s.Scrape(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "listing servers")
	assert.Equal(t, 0, md.ResourceMetrics().Len())
}

func TestScrapeServerMetricErrorSkipsServer(t *testing.T) {
	mock := &mockAPI{
		servers: []*hcloud.Server{
			testServer(1, "web-1", hcloud.ServerStatusRunning),
			testServer(3, "web-3", hcloud.ServerStatusRunning),
		},
		serverMetricErr: fmt.Errorf("metrics unavailable"),
	}

	s := &hetznerScraper{
		cfg: &Config{
			Servers:       true,
			LoadBalancers: false,
			MetricsStep:   60,
		},
		logger: zap.NewNop(),
		api:    mock,
	}

	md, err := s.Scrape(context.Background())
	require.NoError(t, err)
	// Both servers still emit resource metrics (running + traffic gauges), just no API metrics
	assert.Equal(t, 2, md.ResourceMetrics().Len())
	for i := 0; i < md.ResourceMetrics().Len(); i++ {
		sm := md.ResourceMetrics().At(i).ScopeMetrics().At(0)
		// 1 running + 3 traffic = 4 (no API metrics due to error)
		assert.Equal(t, 4, sm.Metrics().Len())
	}
}

func TestScrapeEmptyTimeSeries(t *testing.T) {
	mock := &mockAPI{
		servers: []*hcloud.Server{
			testServer(1, "web-1", hcloud.ServerStatusRunning),
		},
		serverMetrics: map[int64]*hcloud.ServerMetrics{
			1: {
				TimeSeries: map[string][]hcloud.ServerMetricsValue{
					"cpu": {}, // empty
				},
			},
		},
	}

	s := &hetznerScraper{
		cfg: &Config{
			Servers:       true,
			LoadBalancers: false,
			MetricsStep:   60,
		},
		logger: zap.NewNop(),
		api:    mock,
	}

	md, err := s.Scrape(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, md.ResourceMetrics().Len())
	// 4 base metrics (running + 3 traffic), no API metrics since the series was empty
	assert.Equal(t, 4, md.ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics().Len())
}

func TestScrapeUnparsableValue(t *testing.T) {
	mock := &mockAPI{
		servers: []*hcloud.Server{
			testServer(1, "web-1", hcloud.ServerStatusRunning),
		},
		serverMetrics: map[int64]*hcloud.ServerMetrics{
			1: {
				TimeSeries: map[string][]hcloud.ServerMetricsValue{
					"cpu": {{Timestamp: 1060, Value: "not-a-number"}},
				},
			},
		},
	}

	s := &hetznerScraper{
		cfg: &Config{
			Servers:       true,
			LoadBalancers: false,
			MetricsStep:   60,
		},
		logger: zap.NewNop(),
		api:    mock,
	}

	md, err := s.Scrape(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, md.ResourceMetrics().Len())
	// 4 base metrics, unparsable API value should be skipped
	assert.Equal(t, 4, md.ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics().Len())
}

func TestScrapeUnknownTimeSeriesKey(t *testing.T) {
	mock := &mockAPI{
		servers: []*hcloud.Server{
			testServer(1, "web-1", hcloud.ServerStatusRunning),
		},
		serverMetrics: map[int64]*hcloud.ServerMetrics{
			1: {
				TimeSeries: map[string][]hcloud.ServerMetricsValue{
					"unknown_metric": {{Timestamp: 1060, Value: "42"}},
				},
			},
		},
	}

	s := &hetznerScraper{
		cfg: &Config{
			Servers:       true,
			LoadBalancers: false,
			MetricsStep:   60,
		},
		logger: zap.NewNop(),
		api:    mock,
	}

	md, err := s.Scrape(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, md.ResourceMetrics().Len())
	// 4 base metrics, unknown API key should be skipped
	assert.Equal(t, 4, md.ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics().Len())
}

func TestScraperStartShutdown(t *testing.T) {
	s := &hetznerScraper{
		cfg:    &Config{},
		logger: zap.NewNop(),
	}

	err := s.Start(context.Background(), nil)
	assert.NoError(t, err)

	err = s.Shutdown(context.Background())
	assert.NoError(t, err)
}

func TestLastTimeSeriesValue(t *testing.T) {
	tests := []struct {
		name     string
		values   []hcloud.ServerMetricsValue
		expected float64
		ok       bool
	}{
		{
			name:     "empty slice",
			values:   nil,
			expected: 0,
			ok:       false,
		},
		{
			name: "single value",
			values: []hcloud.ServerMetricsValue{
				{Timestamp: 1000, Value: "42.5"},
			},
			expected: 42.5,
			ok:       true,
		},
		{
			name: "multiple values returns last",
			values: []hcloud.ServerMetricsValue{
				{Timestamp: 1000, Value: "10"},
				{Timestamp: 1060, Value: "20"},
				{Timestamp: 1120, Value: "30"},
			},
			expected: 30,
			ok:       true,
		},
		{
			name: "unparsable value",
			values: []hcloud.ServerMetricsValue{
				{Timestamp: 1000, Value: "invalid"},
			},
			expected: 0,
			ok:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, ok := lastTimeSeriesValue(tt.values)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.expected, v)
		})
	}
}

func TestScrapePartialFailure(t *testing.T) {
	// Server listing fails but LB listing succeeds
	mock := &mockAPI{
		serverErr: fmt.Errorf("server api error"),
		loadBalancers: []*hcloud.LoadBalancer{
			testLoadBalancer(10, "lb-1"),
		},
		lbMetrics: map[int64]*hcloud.LoadBalancerMetrics{
			10: {
				TimeSeries: map[string][]hcloud.LoadBalancerMetricsValue{
					"open_connections": {{Timestamp: 1060, Value: "42"}},
				},
			},
		},
	}

	s := &hetznerScraper{
		cfg: &Config{
			Servers:       true,
			LoadBalancers: true,
			MetricsStep:   60,
		},
		logger: zap.NewNop(),
		api:    mock,
	}

	md, err := s.Scrape(context.Background())
	// Should succeed with partial data (LB metrics present)
	require.NoError(t, err)
	assert.Equal(t, 1, md.ResourceMetrics().Len())
}

func TestServerResourceAttributes(t *testing.T) {
	mock := &mockAPI{
		servers: []*hcloud.Server{
			testServer(1, "web-1", hcloud.ServerStatusRunning),
		},
		serverMetrics: map[int64]*hcloud.ServerMetrics{
			1: {
				TimeSeries: map[string][]hcloud.ServerMetricsValue{
					"cpu": {{Timestamp: 1060, Value: "10"}},
				},
			},
		},
	}

	s := &hetznerScraper{
		cfg: &Config{
			Servers:       true,
			LoadBalancers: false,
			MetricsStep:   60,
		},
		logger: zap.NewNop(),
		api:    mock,
	}

	md, err := s.Scrape(context.Background())
	require.NoError(t, err)

	attrs := md.ResourceMetrics().At(0).Resource().Attributes()

	expected := map[string]string{
		"cloud.provider":          "hetzner",
		"cloud.platform":          "hetzner_cloud",
		"host.id":                 "1",
		"host.name":               "web-1",
		"host.type":               "cx22",
		"host.ip":                 "1.2.3.4",
		"cloud.availability_zone": "fsn1-dc14",
		"cloud.region":            "fsn1",
		"hetzner.server.status":   "running",
		"hetzner.server.type.cpu_type": "shared",
		"hetzner.label.env":       "test",
	}

	for key, want := range expected {
		v, ok := attrs.Get(key)
		require.True(t, ok, "missing attribute %s", key)
		assert.Equal(t, want, v.Str(), "attribute %s", key)
	}

	// Check numeric attributes
	v, ok := attrs.Get("hetzner.server.type.cores")
	require.True(t, ok)
	assert.Equal(t, int64(2), v.Int())

	v, ok = attrs.Get("hetzner.server.type.memory")
	require.True(t, ok)
	assert.Equal(t, float64(4), v.Double())

	v, ok = attrs.Get("hetzner.server.type.disk")
	require.True(t, ok)
	assert.Equal(t, int64(40), v.Int())
}

func TestScrapeMultiDiskNetwork(t *testing.T) {
	mock := &mockAPI{
		servers: []*hcloud.Server{
			testServer(1, "web-1", hcloud.ServerStatusRunning),
		},
		serverMetrics: map[int64]*hcloud.ServerMetrics{
			1: {
				TimeSeries: map[string][]hcloud.ServerMetricsValue{
					"cpu":                     {{Timestamp: 1060, Value: "15"}},
					"disk.0.iops.read":        {{Timestamp: 1060, Value: "100"}},
					"disk.0.iops.write":       {{Timestamp: 1060, Value: "50"}},
					"disk.1.iops.read":        {{Timestamp: 1060, Value: "200"}},
					"disk.1.iops.write":       {{Timestamp: 1060, Value: "75"}},
					"network.0.bandwidth.in":  {{Timestamp: 1060, Value: "1000"}},
					"network.0.bandwidth.out": {{Timestamp: 1060, Value: "2000"}},
					"network.1.bandwidth.in":  {{Timestamp: 1060, Value: "3000"}},
					"network.1.bandwidth.out": {{Timestamp: 1060, Value: "4000"}},
				},
			},
		},
	}

	s := &hetznerScraper{
		cfg: &Config{
			Servers:       true,
			LoadBalancers: false,
			MetricsStep:   60,
		},
		logger: zap.NewNop(),
		api:    mock,
	}

	md, err := s.Scrape(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, md.ResourceMetrics().Len())

	sm := md.ResourceMetrics().At(0).ScopeMetrics().At(0)
	// 1 running + 3 traffic + 1 cpu + 4 disk + 4 network = 13
	assert.Equal(t, 13, sm.Metrics().Len())

	// Collect metrics with their data point attributes
	type metricPoint struct {
		value float64
		attrs map[string]string
	}
	metrics := make(map[string][]metricPoint)
	for i := 0; i < sm.Metrics().Len(); i++ {
		m := sm.Metrics().At(i)
		dp := m.Gauge().DataPoints().At(0)
		point := metricPoint{value: dp.DoubleValue(), attrs: make(map[string]string)}
		dp.Attributes().Range(func(k string, v pcommon.Value) bool {
			point.attrs[k] = v.Str()
			return true
		})
		metrics[m.Name()] = append(metrics[m.Name()], point)
	}

	// Verify disk metrics have disk_index attribute
	diskReads := metrics["hetzner.server.disk.iops.read"]
	assert.Len(t, diskReads, 2)
	foundDisk0, foundDisk1 := false, false
	for _, dp := range diskReads {
		switch dp.attrs["disk_index"] {
		case "0":
			assert.Equal(t, 100.0, dp.value)
			foundDisk0 = true
		case "1":
			assert.Equal(t, 200.0, dp.value)
			foundDisk1 = true
		}
	}
	assert.True(t, foundDisk0, "missing disk_index=0")
	assert.True(t, foundDisk1, "missing disk_index=1")

	// Verify network metrics have network_index attribute
	netIn := metrics["hetzner.server.network.bandwidth.in"]
	assert.Len(t, netIn, 2)
	foundNet0, foundNet1 := false, false
	for _, dp := range netIn {
		switch dp.attrs["network_index"] {
		case "0":
			assert.Equal(t, 1000.0, dp.value)
			foundNet0 = true
		case "1":
			assert.Equal(t, 3000.0, dp.value)
			foundNet1 = true
		}
	}
	assert.True(t, foundNet0, "missing network_index=0")
	assert.True(t, foundNet1, "missing network_index=1")
}

func TestParseIndexedMetricKey(t *testing.T) {
	tests := []struct {
		key          string
		resourceType string
		index        string
		suffix       string
	}{
		{"disk.0.iops.read", "disk", "0", "iops.read"},
		{"disk.2.bandwidth.write", "disk", "2", "bandwidth.write"},
		{"network.1.bandwidth.in", "network", "1", "bandwidth.in"},
		{"network.0.pps.out", "network", "0", "pps.out"},
		{"cpu", "", "", ""},
		{"unknown.0.foo", "", "", ""},
		{"disk.abc.iops.read", "", "", ""},
		{"disk", "", "", ""},
		{"disk.0", "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			rt, idx, sfx := parseIndexedMetricKey(tt.key)
			assert.Equal(t, tt.resourceType, rt)
			assert.Equal(t, tt.index, idx)
			assert.Equal(t, tt.suffix, sfx)
		})
	}
}

func TestScrapeDisabledServers(t *testing.T) {
	s := &hetznerScraper{
		cfg: &Config{
			Servers:       false,
			LoadBalancers: false,
			MetricsStep:   60,
			CollectionInterval: time.Minute,
		},
		logger: zap.NewNop(),
		api:    &mockAPI{},
	}

	md, err := s.Scrape(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, md.ResourceMetrics().Len())
}
