package hetznerreceiver

import (
	"context"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/scraper"
	"go.opentelemetry.io/collector/scraper/scraperhelper"
	"go.uber.org/zap"
)

type hetznerReceiver struct {
	cfg             *Config
	settings        receiver.Settings
	metricsConsumer consumer.Metrics
	logger          *zap.Logger
	scraper         *hetznerScraper
	sController     receiver.Metrics
}

func newHetznerReceiver(
	params receiver.Settings,
	cfg *Config,
	metricsConsumer consumer.Metrics,
) *hetznerReceiver {
	return &hetznerReceiver{
		cfg:             cfg,
		settings:        params,
		metricsConsumer: metricsConsumer,
		logger:          params.Logger,
	}
}

func (r *hetznerReceiver) Start(ctx context.Context, host component.Host) error {
	client := hcloud.NewClient(hcloud.WithToken(r.cfg.APIToken))

	r.scraper = &hetznerScraper{
		cfg:    r.cfg,
		logger: r.logger,
		api:    &hcloudClient{client: client},
	}

	scrp, err := scraper.NewMetrics(
		r.scraper.Scrape,
		scraper.WithStart(r.scraper.Start),
		scraper.WithShutdown(r.scraper.Shutdown),
	)
	if err != nil {
		return err
	}

	r.sController, err = scraperhelper.NewMetricsController(
		&scraperhelper.ControllerConfig{
			CollectionInterval: r.cfg.CollectionInterval,
		},
		r.settings,
		r.metricsConsumer,
		scraperhelper.AddScraper(typeStr, scrp),
	)
	if err != nil {
		return err
	}

	return r.sController.Start(ctx, host)
}

func (r *hetznerReceiver) Shutdown(ctx context.Context) error {
	if r.sController != nil {
		return r.sController.Shutdown(ctx)
	}
	return nil
}
