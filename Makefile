# GrowRig Platform — build orchestration for Grow Core (Go) and the web app.
#
#   make dev-core     run Grow Core with the offline simulator
#   make dev-web      run the SvelteKit dev server
#   make build        build the web app, embed it, and produce a single binary
#   make run          build then run the single binary (simulator)
#   make test         Go tests + web type-check
#   make clean        remove build artifacts and local databases

BIN     ?= bin/growcore
DIST     = growcore/internal/webui/dist

.DEFAULT_GOAL := help

.PHONY: help
help:
	@grep -E '^#   make' Makefile | sed 's/^#   /  /'

# --- development ---

.PHONY: dev-core
dev-core:
	cd growcore && go run ./cmd/growcore -config growcore.sim.yaml

.PHONY: dev-web
dev-web:
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

.PHONY: build
build: embed
	cd growcore && go build -o ../$(BIN) ./cmd/growcore
	@echo "built $(BIN)"

.PHONY: run
run: build
	./$(BIN) -config growcore/growcore.sim.yaml

# --- quality ---

.PHONY: test
test:
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
	rm -f growcore/*.db
