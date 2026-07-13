# GrowRig Platform — build orchestration for Grow Core (Go) and the web app.
#
# Local secrets and overrides live in .env.local at the repo root (gitignored).
# Set GROWCORE_PORT in .env.local (or pass on the CLI) only to override the port
# in growcore.dev.yaml for both Grow Core and the Vite dev server.
#
#   make dev                       run Grow Core + SvelteKit dev server
#   make dev GROWCORE_PORT=8791    override backend port for this session
#   make dev-core                  run Grow Core against your Home Assistant
#   make dev-core-sim              run Grow Core with the offline simulator
#   make dev-web                   run the SvelteKit dev server
#   make build        build the web app, embed it, and produce a single binary
#   make run          build then run the single binary (simulator)
#   make addon        cross-compile binaries for the manual HA add-on (addon/growrig)
#   make test         Go tests + web type-check
#   make clean        remove build artifacts and local databases

BIN          ?= bin/growcore
DIST          = growcore/internal/webui/dist
CATALOG_SRC   = catalog/devices
CATALOG_DATA  = growcore/internal/catalog/data
VENDOR_SRC    = catalog/vendors
VENDOR_DATA   = growcore/internal/catalog/vendor-data
SPECIES_SRC   = catalog/species
SPECIES_DATA  = growcore/internal/species/data
INVENTORY_SRC  = catalog/inventory
INVENTORY_DATA = growcore/internal/inventory/data
INTEGRATIONS_SRC = catalog/integrations
INTEGRATIONS_DATA = growcore/internal/integrations/data
CONFIG_DEV   ?= growcore.dev.yaml
CONFIG_SIM    = growcore/growcore.sim.yaml

# Load root .env.local (GROWCORE_HA_TOKEN, GROWCORE_PORT, …).
ifneq (,$(wildcard .env.local))
include .env.local
export
endif

# Port for Vite → Grow Core. Defaults to server.addr in $(CONFIG_DEV); override
# in .env.local or on the CLI only when you need a different port than the YAML.
CONFIG_PORT := $(shell grep -A1 '^server:' $(CONFIG_DEV) 2>/dev/null | grep 'addr:' | sed -E 's/.*:([0-9]+).*/\1/')

ifdef GROWCORE_ADDR
ADDR_FLAG = -addr $(GROWCORE_ADDR)
GROWCORE_PORT := $(lastword $(subst :, ,$(GROWCORE_ADDR)))
else ifdef GROWCORE_PORT
ADDR_FLAG = -addr :$(GROWCORE_PORT)
else
GROWCORE_PORT := $(if $(CONFIG_PORT),$(CONFIG_PORT),8080)
ADDR_FLAG :=
endif

VITE_GROWCORE_URL ?= http://localhost:$(GROWCORE_PORT)
export VITE_GROWCORE_URL

.DEFAULT_GOAL := help

.PHONY: help
help:
	@grep -E '^#   make' Makefile | sed 's/^#   /  /'

# --- development ---

.PHONY: dev
dev:
	@echo "Starting Grow Core on :$(GROWCORE_PORT) ($(CONFIG_DEV)) + web dev server"
	@trap 'kill 0' INT TERM EXIT; \
	$(MAKE) dev-core & \
	$(MAKE) dev-web & \
	wait

.PHONY: dev-core
dev-core: catalog-check
	cd growcore && go run ./cmd/growcore -config ../$(CONFIG_DEV) $(ADDR_FLAG)

.PHONY: dev-core-sim
dev-core-sim: catalog-check
	cd growcore && go run ./cmd/growcore -config growcore.sim.yaml $(ADDR_FLAG)

.PHONY: dev-web
dev-web: web-deps
	cd web && npm run dev

# --- production build (single embedded binary) ---

.PHONY: web-deps
web-deps:
	cd web && npm install

.PHONY: web-build
web-build: web-deps
	cd web && npm run build

.PHONY: embed
embed: web-build
	find $(DIST) -mindepth 1 ! -name .gitkeep -delete
	cp -r web/build/. $(DIST)/

# Sync the catalog submodule into the Go module so the default content is
# embedded in the single binary. Fail with a useful hint when the submodule
# was not initialized by a non-recursive clone.
.PHONY: catalog-check
catalog-check:
	@test -f catalog/catalog.yaml || { \
		echo "catalog submodule is missing; run: git submodule update --init" >&2; \
		exit 1; \
	}

.PHONY: catalog-embed
catalog-embed: catalog-check
	find $(CATALOG_DATA) -mindepth 1 ! -name .gitkeep -delete
	cp -r $(CATALOG_SRC)/. $(CATALOG_DATA)/
	find $(VENDOR_DATA) -mindepth 1 ! -name .gitkeep -delete
	cp -r $(VENDOR_SRC)/. $(VENDOR_DATA)/
	find $(SPECIES_DATA) -mindepth 1 ! -name .gitkeep -delete
	cp -r $(SPECIES_SRC)/. $(SPECIES_DATA)/
	find $(INVENTORY_DATA) -mindepth 1 ! -name .gitkeep -delete
	cp -r $(INVENTORY_SRC)/. $(INVENTORY_DATA)/
	find $(INTEGRATIONS_DATA) -mindepth 1 ! -name .gitkeep -delete
	cp -r $(INTEGRATIONS_SRC)/. $(INTEGRATIONS_DATA)/

.PHONY: build
build: embed catalog-embed
	cd growcore && go build -o ../$(BIN) ./cmd/growcore
	@echo "built $(BIN)"

.PHONY: run
run: build
	./$(BIN) -config $(CONFIG_SIM)

# --- Home Assistant add-on (manual install) ---

# Cross-compile the arch-matched binaries the local HA add-on ships in bin/.
# Each is a static (CGO-free) Linux binary with the web UI + catalogue embedded.
# HA arch -> GOARCH[/GOARM]: aarch64=arm64, amd64=amd64, armv7=arm/7.
ADDON_DIR = addon/growrig
ADDON_BIN = $(ADDON_DIR)/bin

.PHONY: addon
addon: embed catalog-embed
	@mkdir -p $(ADDON_BIN)
	@set -e; \
	for spec in "aarch64 arm64 " "amd64 amd64 " "armv7 arm 7"; do \
	  set -- $$spec; ha=$$1; goarch=$$2; goarm=$$3; \
	  echo "building $(ADDON_BIN)/growcore.$$ha (GOARCH=$$goarch GOARM=$$goarm)"; \
	  (cd growcore && CGO_ENABLED=0 GOOS=linux GOARCH=$$goarch GOARM=$$goarm \
	    go build -trimpath -ldflags "-s -w" -o ../$(ADDON_BIN)/growcore.$$ha ./cmd/growcore); \
	done
	@echo "add-on binaries ready in $(ADDON_BIN)/ — copy $(ADDON_DIR)/ to your HAOS /addons share"

# --- quality ---

.PHONY: test
test: catalog-check
	cd growcore && go test ./...
	cd web && npm run check

.PHONY: fmt
fmt:
	cd growcore && gofmt -w .

# --- housekeeping ---

.PHONY: clean
clean:
	rm -rf bin web/build web/.svelte-kit
	find $(DIST) -mindepth 1 ! -name .gitkeep -delete
	find $(CATALOG_DATA) -mindepth 1 ! -name .gitkeep -delete
	find $(VENDOR_DATA) -mindepth 1 ! -name .gitkeep -delete
	find $(SPECIES_DATA) -mindepth 1 ! -name .gitkeep -delete
	find $(INVENTORY_DATA) -mindepth 1 ! -name .gitkeep -delete
	find $(INTEGRATIONS_DATA) -mindepth 1 ! -name .gitkeep -delete
	rm -f growcore/*.db growcore.dev.db growcore.dev-local.db
