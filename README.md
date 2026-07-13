# GrowRig Platform

Grow Core and the Grow App Web — the software half of [GrowRig](../growrig),
an open-source, local-first ecosystem for controlled indoor growing.

This repository currently implements the **Phase 2 vertical slice** from the
[roadmap](../growrig/ROADMAP.md): a Go control engine with persistent storage, a
built-in simulator, a live API, and a SvelteKit dashboard. It runs end-to-end
**without any hardware** — the simulator stands in for a Grow Controller so you
can see the whole system working before flashing a single ESP32.

```
growrig-platform/
├── growcore/   # Grow Core — Go control engine + API (see growcore/)
└── web/        # Grow App Web — SvelteKit + Tailwind dashboard
```

## What works today

- **Domain model** — environments with climate targets; devices with fan
  channels mapped to semantic roles (exhaust / intake / circulation).
- **Reconciliation engine** — a proportional control law turns *current vs.
  target temperature* into desired fan speeds, with an emergency-temperature
  override that forces every fan to 100%.
- **Adapters** — a **Home Assistant** adapter (reads climate sensors over the
  HA WebSocket API, commands fans via `fan.set_percentage`) and a built-in
  **simulator** (a small physical model where temperature responds to fan
  speed) — selected purely by config.
- **YAML configuration** — one binary, two deployments: HAOS add-on (via the
  Supervisor proxy) or remote HA for local dev, differing only by config.
- **Persistence** — SQLite (pure-Go driver, no CGO) stores configuration and
  climate history.
- **Live API** — REST for configuration + a WebSocket that streams the full
  system snapshot every control tick.
- **Web dashboard** — live temperature/humidity sparklines with target lines,
  controller health, per-fan speed/RPM, and a Setup page for targets and role
  mapping.
- **External integrations** — reusable YAML bundles, encrypted configured
  instances, typed capabilities, health tests, and feature bindings. The first
  bundles support Ollama, generic notification webhooks, and the automatically
  configured Open-Meteo weather provider.

## Quick start (offline simulator)

The default content catalog (devices, integrations, species, inventory,
vendors) lives in [growrig-catalog](https://github.com/growrig/growrig-catalog),
linked here as a git submodule at `catalog/` — clone with
`git clone --recurse-submodules`, or run `git submodule update --init` in an
existing checkout.

Admins can also add public repositories with a compatible `catalog.yaml`
manifest under **Control panel → Catalog**. Custom device and integration
entries are cached beside the Grow Core database and merged without requiring
a rebuild.

No Home Assistant or hardware required — two processes:

**1. Grow Core** (Go 1.24+):

```bash
cd growcore
go run ./cmd/growcore -config growcore.sim.yaml   # listens on :8080
```

**2. Grow App Web** (Node 20+):

```bash
cd web
npm install
npm run dev                      # http://localhost:5173
```

The web app talks to Grow Core at `http://localhost:8080` by default. To point
it elsewhere, set `VITE_GROWCORE_URL` (see [`web/.env.example`](web/.env.example)).

Open the dashboard, go to **Setup**, and lower the temperature target below the
current reading — the exhaust fan will ramp up on the dashboard within a second.

## Install on Home Assistant

Grow Core ships as a **local Home Assistant OS add-on** in
[`addon/growrig/`](addon/growrig/). Build the arch-matched binaries and copy the
folder onto your HAOS host:

```bash
make addon        # cross-compiles addon/growrig/bin/growcore.{aarch64,amd64,armv7}
```

Copy `addon/growrig/` to the `addons` share (`/addons/growrig/`), then in Home
Assistant open **Settings → Add-ons → Add-on Store → ⋮ → Check for updates** and
install **GrowRig — Grow Core** from *Local add-ons*. The add-on reaches Home
Assistant through the Supervisor proxy (no token needed) and serves the
dashboard on host port `8099` by default. See
[`addon/growrig/README.md`](addon/growrig/README.md) for details.

## Configuration

Grow Core is configured with YAML. The same binary runs in three modes,
selected by `adapter.type` and the config file you pass with `-config`:

| File | Mode | Home Assistant |
|---|---|---|
| [`growcore.yaml`](growcore/growcore.yaml) | **Default — HAOS add-on** | Supervisor proxy (`http://supervisor/core`, `$SUPERVISOR_TOKEN`) |
| [`growcore.dev.yaml`](growcore/growcore.dev.yaml) | Local dev vs. remote HA | `http://homeassistant.local:8123` + long-lived token |
| [`growcore.sim.yaml`](growcore/growcore.sim.yaml) | Offline simulator | none |

`${ENV_VAR}` references in the file are expanded at load, so tokens stay out of
version control. For local development against your own Home Assistant:

```bash
export GROWCORE_HA_TOKEN=eyJ...          # HA → Profile → Long-lived access tokens
go run ./cmd/growcore -config growcore.dev.yaml
```

The config declares environments, devices, and how each device's sensors and
fan channels bind to Home Assistant entities. Edit the `sensor.*` / `fan.*`
entity ids to match your ESPHome controller. Running with no config file at all
uses the built-in simulator defaults.

## Grow Core API

Base URL `http://localhost:8080`.

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/health` | Liveness probe |
| `GET` | `/api/state` | Latest full snapshot |
| `GET` | `/api/roles` | Assignable channel roles |
| `GET` | `/api/environments` | List environments |
| `GET` | `/api/catalog` | Device catalogue (including vendor and image metadata) |
| `GET` | `/api/vendors` | Vendor catalogue and logo paths |
| `GET` | `/api/integration-bundles` | Available external-service integrations |
| `GET/POST` | `/api/integration-instances` | List or create configured instances (admin) |
| `POST` | `/api/integration-instances/{id}/test` | Test an instance connection (admin) |
| `GET/POST` | `/api/integration-bindings` | List or set feature bindings (admin) |
| `PUT` | `/api/environments/{id}/targets` | Set `{targetTempC, targetHumidity}` |
| `GET` | `/api/environments/{id}/history?limit=120` | Climate history (oldest first) |
| `GET` | `/api/devices` | Devices with live values + roles |
| `PUT` | `/api/devices/{id}/channels/{ch}/role` | Assign `{role}` to a channel |
| `GET` | `/api/ws` | WebSocket: streams a snapshot each control tick |

Flags: `-config growcore.yaml` (config path), `-addr :8080` (overrides
`server.addr`). All other settings — storage path, control interval, adapter —
come from the config file.

## Architecture notes

Grow Core is structured as the docs describe — a reconciliation engine that is
independent of any single adapter:

```
growcore/internal/
├── config/     # YAML config: modes, adapters, device topology & entity bindings
├── domain/     # semantic model: environment, device, channel, role
├── control/    # pure control law + reconciliation loop + Adapter interface
├── sim/        # simulator adapter
├── ha/         # Home Assistant adapter (WebSocket state + REST commands)
├── integrations/ # external-service bundles, secrets and capability runtimes
├── store/      # SQLite persistence
└── api/        # HTTP + WebSocket
```

Adapters implement `control.Adapter`, so the engine and the pure control law
(`control.ChannelSpeed`, unit tested via `go test ./...`) are identical whether
devices are simulated or reached through Home Assistant. New adapters (e.g.
direct MQTT) slot in behind the same interface without touching domain logic.

External services are deliberately separate from adapters and devices. Bundle
definitions live under `catalog/integrations/<category>/<id>/integration.yaml`; users
configure instances under **Control panel → Integrations**. Secret fields are
AES-GCM encrypted using a local `0600` key beside the Grow Core database and
are never returned by the API. Production builds embed the bundle tree while
development loads it directly from disk.

### Deviations from the target design

- **Storage** uses the pure-Go `modernc.org/sqlite` driver (no CGO) rather than
  a CGO build, to keep cross-compilation for the Grow Hub trivial. Schema and
  API are unchanged.
- The Home Assistant adapter uses **`fan.set_percentage`** for commands. If your
  ESPHome PWM outputs are exposed as `number`/`light` entities instead of `fan`
  entities, that service call needs adjusting.

## Next steps (per roadmap)

- Direct MQTT adapter with one authoritative adapter per controller (Phase 3).
- Controller health/presence and command timeout surfaced in the UI.
- Recipes and cycles (phase-based targets over time).
- Manual overrides and alerts.
