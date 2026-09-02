# AGENTS.md

Guidance for agents working in this repository. (`CLAUDE.md` is a symlink to this file.)

## Project Overview

A custom OpenTelemetry Collector receiver for Hetzner Cloud: it polls the
Hetzner Cloud API via the hcloud-go v2 SDK for servers and load balancers and
converts the results to OTel metrics.

## Build, Test, Run

```bash
# Build the collector binary into ./build (ocb is gitignored - download the
# OpenTelemetry Collector Builder matching otelcol_version in builder-config.yaml)
./ocb --config builder-config.yaml

cd hetznerreceiver && go test ./...        # tests live only in this module

export HETZNER_API_TOKEN="your-token-here"
./build/otelcol-hetzner --config example/config.yaml
```

Config options are documented once, in `hetznerreceiver/doc.go` and
`example/config.yaml`; dependency versions in `hetznerreceiver/go.mod` and
`builder-config.yaml`. Do not restate either here.

## Architecture

Package `hetznerreceiver/`: `factory.go` (registers component type `hetzner`),
`config.go`, `receiver.go` (hcloud client + scraperhelper controller),
`scraper.go` (`Scrape()`), `metrics.go` (metric name/unit maps, gauge helper,
attribute setters).

```
Hetzner Cloud API → hcloud-go SDK → Scrape() → OTel metrics → Exporter pipeline
```

Tests operate at the SDK interface level, not over HTTP: the `hcloudAPI`
interface in `scraper.go` abstracts the four SDK calls used, and `mockAPI` in
the tests implements it. Keep new SDK usage behind that interface so it stays
mockable.

## CI

`.github/workflows/ci.yml` runs vet/build/test in `hetznerreceiver/` on
push/PR to `main`. `.github/workflows/release.yml` is manually dispatched with
a `version` input: it re-runs the same gate, then a job gated on the
`production` GitHub Environment (required reviewer approval) tags that exact
SHA and cuts the Release. **There must be no push-to-tag trigger** - tagging
happens only inside the approved job, so a release cannot be cut without CI
passing on the exact commit plus approval.

## Resource Attribute Conventions

One receiver instance emits metrics for many Hetzner resources, so never
assume a single "collector host" identity applies to the data. Each resource
gets its own `ResourceMetrics` with identity taken from the Hetzner API
response for *that* resource, never from the collector's own environment:

- **Servers** are real hosts: `host.id` / `host.name` / `host.type` /
  `host.ip` come from the server's own API fields
  (`setServerResourceAttributes`).
- **Load balancers are not hosts** - a managed PaaS product with no underlying
  machine to name. Never invent a `host.name` for them; their identity is
  `hetzner.load_balancer.id` / `.name` (`setLBResourceAttributes`).
- `deployment.environment.name` is never hardcoded here - the Hetzner API has
  no concept of environment. It comes from the optional `environment` config
  field, and is omitted entirely when empty rather than emitted blank.

Do not paper over a missing `deployment.environment` / `host.name` at the
collector-config level (e.g. a processor inserting the collector host's own
`host.name`) - that mislabels every series with one machine's identity.
Resource-identity fixes belong at the receiver, where per-resource API data is
still available.

## Per-Target Metric Identity (Load Balancer Targets)

`hetzner.load_balancer.target.healthy` carries target identity as *data point*
attributes, not resource attributes, because one LB resource has many targets.
Health lives on a label_selector target's expanded member servers
(`hcloud.LoadBalancerTarget.Targets`), never on the selector entry itself, so
emission recurses into them. A target with no `HealthStatus` entries emits
nothing, keeping "no target" distinguishable from a target reporting
`...StatusUnknown` (emitted with `status` = `unknown`). See
`addLBTargetHealthGauges` in `metrics.go` for the exact attribute set.

## Maintaining this file

Keep this file for knowledge useful to almost every future agent session in this project.
Do not repeat what the codebase already shows; point to the authoritative file or command instead.
Prefer rewriting or pruning existing entries over appending new ones.
When updating this file, preserve this bar for all agents and keep entries concise.
