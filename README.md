# GrowRig

Grow Core and Grow App Web — the software behind **GrowRig**, an open-source,
local-first platform for monitoring and automating controlled indoor growing. A
Go control engine keeps each grow environment on its climate targets, and a
SvelteKit dashboard makes every decision visible. It integrates with Home
Assistant and runs end-to-end with no hardware through a built-in simulator.

Documentation and guides: **[growrig.dev](https://growrig.dev)**.

```
growrig/
├── growcore/   # Grow Core — Go control engine + HTTP/WebSocket API
└── web/        # Grow App Web — SvelteKit + Tailwind dashboard
```

## Features

- **Semantic domain model** — grow environments with climate targets, and
  devices whose channels map to roles (exhaust, intake, circulation) rather than
  to a specific vendor.
- **Reconciliation engine** — a proportional control law drives fan speeds from
  the gap between current and target temperature, with an emergency override
  that forces full exhaust.
- **Pluggable adapters** — a Home Assistant adapter (climate over the WebSocket
  API, commands via `fan.set_percentage`) and a physics-based simulator, chosen
  by configuration alone. New transports slot in behind a single interface.
- **Content catalog** — supported devices, integrations, species, inventory and
  vendors ship as a versioned catalog, and installations can add their own
  catalog sources at runtime. See
  [growrig-catalog](https://github.com/growrig/growrig-catalog).
- **External integrations** — reusable, typed integration bundles with encrypted
  credentials, connection tests and feature bindings; bundled providers include
  Ollama, notification webhooks and Open-Meteo weather.
- **Persistence** — configuration and climate history in SQLite (pure-Go, no CGO).
- **Live API** — REST for configuration, plus a WebSocket that streams the full
  system snapshot on every control tick.
- **Dashboard** — live temperature and humidity trends against targets,
  controller health, per-fan speed and RPM, and setup for targets and roles.

## Quick start

The default content catalog lives in
[growrig-catalog](https://github.com/growrig/growrig-catalog) and is linked as a
submodule at `catalog/`; clone with `--recurse-submodules`, or run
`git submodule update --init` in an existing checkout. No Home Assistant or
hardware is required — the simulator stands in for a controller.

**Grow Core** (Go 1.26+):

```bash
cd growcore
go run ./cmd/growcore -config growcore.sim.yaml   # listens on :8080
```

**Grow App Web** (Node 20+):

```bash
cd web
npm install
npm run dev                                        # http://localhost:5173
```

The dashboard talks to Grow Core at `http://localhost:8080`; override with
`VITE_GROWCORE_URL` (see [`web/.env.example`](web/.env.example)). Open **Setup**
and lower a temperature target below the current reading — the exhaust fan ramps
up on the dashboard within a second.

## Home Assistant add-on

Grow Core ships as a local Home Assistant OS add-on in
[`addon/growrig/`](addon/growrig/):

```bash
make addon        # cross-compiles the arch-matched binaries
```

Copy `addon/growrig/` to the HAOS `addons` share, then install **GrowRig — Grow
Core** from *Local add-ons*. The add-on reaches Home Assistant through the
Supervisor proxy (no token needed) and serves the dashboard on host port `8099`.
See [`addon/growrig/README.md`](addon/growrig/README.md) for details.

## Configuration

Grow Core is configured with YAML; the same binary runs in three modes, selected
by the `-config` file:

| File | Mode | Home Assistant |
|---|---|---|
| [`growcore.yaml`](growcore/growcore.yaml) | HAOS add-on (default) | Supervisor proxy (`http://supervisor/core`, `$SUPERVISOR_TOKEN`) |
| [`growcore.dev.yaml`](growcore/growcore.dev.yaml) | Local dev vs. remote HA | `http://homeassistant.local:8123` + long-lived token |
| [`growcore.sim.yaml`](growcore/growcore.sim.yaml) | Offline simulator | none |

`${ENV_VAR}` references are expanded at load, so tokens stay out of version
control. The config declares environments, devices, and how each device's
sensors and fan channels bind to Home Assistant entities.

```bash
export GROWCORE_HA_TOKEN=…    # HA → Profile → Long-lived access tokens
go run ./cmd/growcore -config growcore.dev.yaml
```

If your ESPHome PWM outputs are exposed as `number` or `light` entities rather
than `fan`, adjust the adapter's service call accordingly.

## API

Base URL `http://localhost:8080`. The full reference lives in the
[documentation](https://growrig.dev/docs/).

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/health` | Liveness probe |
| `GET` | `/api/state` | Latest full snapshot |
| `GET` | `/api/roles` | Assignable channel roles |
| `GET` | `/api/environments` | List environments |
| `GET` | `/api/catalog` | Device catalog (including vendor and image metadata) |
| `GET` | `/api/vendors` | Vendor catalog and logo paths |
| `GET` | `/api/integration-bundles` | Available external-service integrations |
| `GET/POST` | `/api/integration-instances` | List or create configured instances (admin) |
| `POST` | `/api/integration-instances/{id}/test` | Test an instance connection (admin) |
| `GET/POST` | `/api/integration-bindings` | List or set feature bindings (admin) |
| `PUT` | `/api/environments/{id}/targets` | Set `{targetTempC, targetHumidity}` |
| `GET` | `/api/environments/{id}/history?limit=120` | Climate history (oldest first) |
| `GET` | `/api/devices` | Devices with live values + roles |
| `PUT` | `/api/devices/{id}/channels/{ch}/role` | Assign `{role}` to a channel |
| `GET` | `/api/ws` | WebSocket: streams a snapshot each control tick |

Flags: `-config` (config path) and `-addr` (overrides `server.addr`); all other
settings — storage path, control interval, adapter — come from the config file.

## Architecture

Grow Core is built around a pure control law that is independent of any single
adapter:

```
growcore/internal/
├── config/        # YAML config: modes, adapters, topology & entity bindings
├── domain/        # semantic model: environment, device, channel, role
├── control/       # pure control law + reconciliation loop + Adapter interface
├── sim/           # simulator adapter
├── ha/            # Home Assistant adapter (WebSocket state + REST commands)
├── catalog/       # supported-device catalog
├── catalogsource/ # runtime custom catalog sources
├── integrations/  # external-service bundles, secrets and capability runtimes
├── store/         # SQLite persistence
└── api/           # HTTP + WebSocket
```

Adapters implement `control.Adapter`, so the engine and the unit-tested control
law (`go test ./...`) behave identically whether devices are simulated or
reached through Home Assistant. External services stay separate from devices:
integration bundles are configured as instances whose secret fields are AES-GCM
encrypted with a local key and never returned by the API.

The [roadmap](https://growrig.dev/docs/roadmap/) tracks what comes next.
