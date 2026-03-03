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
		Labels: map[string]string{"env": "test"},
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
		Targets:  []hcloud.LoadBalancerTarget{{}, {}, {}},
		Labels:   map[string]string{"env": "prod"},
	}
}

func TestScrapeServers(t *testing.T) {
	mock := &mockAPI{
		servers: []*hcloud.Server{
			testServer(1, "web-1", hcloud.ServerStatusRunning),
			testServer(2, "web-2", hcloud.ServerStatusOff), // should be skipped
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

	// Only 1 resource (running server), not 2
	require.Equal(t, 1, md.ResourceMetrics().Len())

	rm := md.ResourceMetrics().At(0)
	attrs := rm.Resource().Attributes()

	// Check resource attributes
	v, ok := attrs.Get("cloud.provider")
	require.True(t, ok)
	assert.Equal(t, "hetzner", v.Str())

	v, ok = attrs.Get("host.name")
	require.True(t, ok)
	assert.Equal(t, "web-1", v.Str())

	v, ok = attrs.Get("host.id")
	require.True(t, ok)
	assert.Equal(t, "1", v.Str())

	v, ok = attrs.Get("host.type")
	require.True(t, ok)
	assert.Equal(t, "cx22", v.Str())

	v, ok = attrs.Get("cloud.region")
	require.True(t, ok)
	assert.Equal(t, "fsn1", v.Str())

	v, ok = attrs.Get("hetzner.label.env")
	require.True(t, ok)
	assert.Equal(t, "test", v.Str())

	// Check metrics
	sm := rm.ScopeMetrics().At(0)
	assert.Equal(t, scopeName, sm.Scope().Name())
	assert.Equal(t, 3, sm.Metrics().Len())

	// Collect metric names and values
	metricValues := make(map[string]float64)
	for i := 0; i < sm.Metrics().Len(); i++ {
		m := sm.Metrics().At(i)
		dp := m.Gauge().DataPoints().At(0)
		metricValues[m.Name()] = dp.DoubleValue()
	}

	assert.Equal(t, 25.3, metricValues["hetzner.server.cpu"])
	assert.Equal(t, 100.0, metricValues["hetzner.server.disk.iops.read"])
	assert.Equal(t, 5000.0, metricValues["hetzner.server.network.bandwidth.in"])
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

	// Check metrics
	sm := rm.ScopeMetrics().At(0)
	assert.Equal(t, 3, sm.Metrics().Len())

	metricValues := make(map[string]float64)
	for i := 0; i < sm.Metrics().Len(); i++ {
		m := sm.Metrics().At(i)
		dp := m.Gauge().DataPoints().At(0)
		metricValues[m.Name()] = dp.DoubleValue()
	}

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
	// Both servers had metric errors, so no resource metrics emitted
	assert.Equal(t, 0, md.ResourceMetrics().Len())
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
	// No metrics since the series was empty
	assert.Equal(t, 0, md.ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics().Len())
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
	// Unparsable value should be skipped
	assert.Equal(t, 0, md.ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics().Len())
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
	// Unknown key should be skipped
	assert.Equal(t, 0, md.ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics().Len())
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
