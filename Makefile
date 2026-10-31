SHELL := /bin/sh
COMPOSE := docker compose

.PHONY: help env build fmt fmt-check vet check docker-build up down restart logs ps seed psql swagger-check clean launch demo

help:
	@printf '%s\n' \
		'make env            Create .env from .env.example' \
		'make build          Compile all Go packages' \
		'make fmt            Format Go source' \
		'make fmt-check      Verify formatting' \
		'make vet            Run go vet' \
		'make check          Run fmt-check, vet, swagger-check, and build' \
		'make docker-build   Build all service images' \
		'make up             Start the complete stack' \
		'make launch         Start, wait for readiness, save logs, and show demo summary' \
		'make demo           Alias for make launch' \
		'make down           Stop the stack' \
		'make restart        Rebuild and restart the stack' \
		'make logs           Follow service logs' \
		'make ps             Show service status' \
		'make seed           Load sample SMS and outbox data' \
		'make psql           Open a PostgreSQL shell' \
		'make swagger-check  Validate docs/swagger.json' \
		'make clean          Remove local build output'

env:
	@test -f .env || cp .env.example .env

build:
	go build ./...

fmt:
	gofmt -w .

fmt-check:
	@test -z "$$(gofmt -l cmd internal docs)" || (gofmt -l cmd internal docs && exit 1)

vet:
	go vet ./...

swagger-check:
	python3 -m json.tool docs/swagger.json >/dev/null

check: fmt-check vet swagger-check build

docker-build:
	$(COMPOSE) build

up: env
	$(COMPOSE) up -d --build

launch: env
	./scripts/launch.fish launch

demo: launch

down:
	$(COMPOSE) down

restart: env
	$(COMPOSE) up -d --build --force-recreate

logs:
	$(COMPOSE) logs -f --tail=200

ps:
	$(COMPOSE) ps

seed:
	$(COMPOSE) exec -T postgres psql -U osyn -d osyn < db/seed.sql

psql:
	$(COMPOSE) exec postgres psql -U osyn -d osyn

clean:
	rm -rf bin
