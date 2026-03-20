SHELL := /bin/bash
.DEFAULT_GOAL := help

GO ?= go
NPM ?= npm
DOCKER_COMPOSE ?= docker compose
ADMIN_WEB_DIR := admin-web
BIN_DIR := bin
BIN_PATH := $(BIN_DIR)/x-tool
GO_MAIN := ./cmd/x-tool

.PHONY: help setup test web-install web-build run build compose-up compose-down compose-logs clean

help:
	@printf "%-16s %s\n" "setup" "Install/update Go and frontend dependencies"
	@printf "%-16s %s\n" "test" "Run Go tests"
	@printf "%-16s %s\n" "web-install" "Install admin-web npm dependencies"
	@printf "%-16s %s\n" "web-build" "Build admin-web into internal/admin/adminui"
	@printf "%-16s %s\n" "run" "Build admin-web and run x-tool locally"
	@printf "%-16s %s\n" "build" "Build admin-web and compile x-tool binary"
	@printf "%-16s %s\n" "compose-up" "Start docker compose stack with rebuild"
	@printf "%-16s %s\n" "compose-down" "Stop docker compose stack"
	@printf "%-16s %s\n" "compose-logs" "Tail docker compose logs"
	@printf "%-16s %s\n" "clean" "Remove local build output"

setup:
	$(GO) mod tidy
	$(MAKE) web-install

test:
	$(GO) test ./...

web-install:
	cd $(ADMIN_WEB_DIR) && $(NPM) install

web-build:
	cd $(ADMIN_WEB_DIR) && $(NPM) run build

run: web-build
	$(GO) run $(GO_MAIN)

build: web-build
	mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN_PATH) $(GO_MAIN)

compose-up:
	$(DOCKER_COMPOSE) up --build

compose-down:
	$(DOCKER_COMPOSE) down

compose-logs:
	$(DOCKER_COMPOSE) logs -f x-tool

clean:
	rm -f $(BIN_PATH)
