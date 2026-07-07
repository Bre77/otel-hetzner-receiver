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

## Resource Attribute Conventions

One receiver instance emits metrics for many Hetzner resources - never assume
a single "collector host" identity applies to the data. Each resource gets
its own `ResourceMetrics` with identity pulled from the Hetzner API response
for *that* resource, never from the collector's own environment:

- **Servers** are real hosts: `host.id` / `host.name` / `host.type` /
  `host.ip` are set from the server's own API fields (`setServerResourceAttributes`
  in `metrics.go`).
- **Load balancers are not hosts** - they're a managed PaaS product with no
  underlying machine to name. Do not invent a `host.name` for them. Their
  identity is `hetzner.load_balancer.id` / `hetzner.load_balancer.name`
  (`setLBResourceAttributes` in `metrics.go`).
- `deployment.environment.name` is never hardcoded in the receiver - the
  Hetzner API has no concept of environment. It's an optional `environment`
  config field (`Config.Environment`) that a deployment sets explicitly;
  when empty, the attribute is omitted entirely rather than emitted blank.

A prior attempt to fix missing `deployment.environment`/`host.name` at the
collector-config level (inserting the collector host's own `host.name` via a
processor) was rejected - it mislabeled every series with one machine's
identity. Any resource-identity fix belongs here, at the receiver, where
per-resource data from the API is still available.
