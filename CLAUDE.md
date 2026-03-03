# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a custom OpenTelemetry Collector receiver for Hetzner Cloud. It polls metrics from the Hetzner Cloud API using the hcloud-go v2 SDK for servers and load balancers, and converts them to OpenTelemetry metrics format.

## Build Commands

```bash
# Build the collector (generates code in build/ and compiles binary)
./ocb --config builder-config.yaml

# Build with verbose output
./ocb --config builder-config.yaml --verbose

# Generate code only, skip compilation
./ocb --config builder-config.yaml --skip-compilation
```

## Test Commands

```bash
# Run all tests
cd hetznerreceiver && go test ./...

# Run specific test
cd hetznerreceiver && go test -run TestScrapeServers

# Run with verbose output
cd hetznerreceiver && go test -v ./...

# Run with coverage
cd hetznerreceiver && go test -cover ./...
```

## Running the Collector

```bash
# Set API token
export HETZNER_API_TOKEN="your-token-here"

# Run with example config
./build/otelcol-hetzner --config example/config.yaml
```

## Architecture

**Core package: `hetznerreceiver/`**

- `factory.go` - Creates the OTel receiver factory, registers component type `hetzner`
- `config.go` - Configuration struct with validation (api_token, intervals, resource toggles)
- `receiver.go` - Main implementation: creates hcloud client, wires scraper to scraperhelper controller
- `scraper.go` - `hetznerScraper.Scrape()` lists resources, fetches metrics, converts to OTel pmetric
- `metrics.go` - Metric name/unit maps, gauge helper, resource attribute setters

**Data flow:**
```
Hetzner Cloud API → hcloud-go SDK → Scrape() → OTel metrics → Exporter pipeline
```

**Interface-based testing:**
- `hcloudAPI` interface abstracts 4 SDK methods (AllServers, GetServerMetrics, AllLoadBalancers, GetLBMetrics)
- `mockAPI` in tests provides deterministic responses
- No HTTP mocking needed - tests operate at the SDK interface level

## Key Dependencies

- OpenTelemetry Collector SDK v0.143.0
- `github.com/hetznercloud/hcloud-go/v2` - Hetzner Cloud Go SDK
