.PHONY: all build rebuild-fresh server desktop sandbox dev db-up db-down migrate test clean cs2-offsets rva-table \
	rva-table-1 rva-table-2 rva-watch frida-rva-full libclient-globals libclient-globals-help \
	start start-fresh run-desktop wait-postgres help

# Строка БД по умолчанию (docker-compose: postgres на хосте :5434). Переопределение: make start DATABASE_URL=...
DATABASE_URL ?= postgres://sfarm:sfarm_dev_pass@127.0.0.1:5434/steam_farm?sslmode=disable

# Панель desktop: по умолчанию 0.0.0.0:8080 (доступ из сети — откройте порт в firewall). Только localhost: SFARM_HTTP_LISTEN=127.0.0.1:0
# Исключить интерфейсы из списка URL в логе: по умолчанию tailscale0; SFARM_HTTP_IFACE_SKIP=- — не исключать; docker0,tailscale0 — свой список
SFARM_HTTP_LISTEN ?= 0.0.0.0:8080
SFARM_HTTP_IFACE_SKIP ?=

# Актуальные offsets.json + client_dll.json из a2x/cs2-dumper (обновлять после патча CS2).
cs2-offsets:
	mkdir -p config/cs2_dumper
	curl -fsSL -o config/cs2_dumper/offsets.json \
		https://raw.githubusercontent.com/a2x/cs2-dumper/main/output/offsets.json
	curl -fsSL -o config/cs2_dumper/client_dll.json \
		https://raw.githubusercontent.com/a2x/cs2-dumper/main/output/client_dll.json
	@echo "OK: config/cs2_dumper/{offsets,client_dll}.json — навигация по памяти подхватит автоматически (Linux)."

# Экспорт so/rva.xlsx → so/rva_table.json (колонки A=RVA, B=block, C=label опционально).
rva-table:
	python3 scripts/so/export_rva_xlsx.py .

# Доп. таблицы: положите so/rva1.xlsx, so/rva2.xlsx и вызовите:
rva-table-1:
	python3 scripts/so/export_rva_xlsx.py . so/rva1.xlsx so/rva_table_1.json

rva-table-2:
	python3 scripts/so/export_rva_xlsx.py . so/rva2.xlsx so/rva_table_2.json

# Длительный Frida-watch (5 мин): FRIDA_PID=$$(pgrep -n cs2) make rva-watch
rva-watch:
	@test -n "$(FRIDA_PID)" || (echo 'Задайте FRIDA_PID=$$(pgrep -n cs2)'; exit 1)
	@test -x .venv-frida/bin/python || (echo 'Создайте .venv-frida'; exit 1)
	.venv-frida/bin/python scripts/so/frida_rva_watch.py $(FRIDA_PID) --duration $(or $(DURATION),300) --table $(or $(RVA_TABLE),so/rva_table.json) --log-file /tmp/frida_rva_watch.log --out-tsv /tmp/frida_rva_watch_summary.tsv --verbose

# См. scripts/so/PIPELINE.md — задачи C–D (нужны CS2 + .venv-frida).
# Пример: FRIDA_PID=$$(pgrep -n cs2) OUT=/tmp/rva.tsv make frida-rva-full
frida-rva-full:
	@test -n "$(FRIDA_PID)" || (echo 'Задайте FRIDA_PID=$$(pgrep -n cs2) и опционально OUT=/tmp/rva.tsv'; exit 1)
	@test -x .venv-frida/bin/python || (echo 'Создайте .venv-frida: python3 -m venv .venv-frida && .venv-frida/bin/pip install frida frida-tools'; exit 1)
	.venv-frida/bin/python scripts/so/frida_rva_table_probe.py $(FRIDA_PID) --limit 70000 --batch 256 --log-file /tmp/frida_rva_full_run.log > $(or $(OUT),/tmp/rva_probe_full.tsv)
	@echo OK: $(or $(OUT),/tmp/rva_probe_full.tsv)

# Анализ TSV → so/libclient_globals_candidates.json (второй аргумент — путь к TSV)
libclient-globals:
	@test -n "$(TSV)" || (echo 'Задайте TSV=/tmp/rva_probe_full.tsv'; exit 1)
	python3 scripts/so/find_libclient_globals.py $(TSV) --offsets-json config/cs2_dumper/offsets.json --out-json so/libclient_globals_candidates.json

libclient-globals-help:
	@echo "Цепочка: make rva-table → FRIDA_PID=\$$(pgrep -n cs2) make frida-rva-full → TSV=/tmp/rva_probe_full.tsv make libclient-globals"
	@echo "Подробно: scripts/so/PIPELINE.md"

all: build

# Full rebuild: no Go/Rust/npm incremental caches (use after meaningful changes)
rebuild-fresh:
	go clean -cache
	cd sandbox-core && cargo clean && cargo build --release
	mkdir -p bin
	cp -f sandbox-core/target/release/sfarm-sandbox bin/sfarm-sandbox
	cd web && rm -rf dist node_modules/.vite && npm ci && npm run build
	mkdir -p cmd/desktop/dist && rm -rf cmd/desktop/dist/* && cp -r web/dist/* cmd/desktop/dist/
	go build -a -o bin/sfarm-server ./cmd/server
	go build -a -o bin/sfarm-desktop ./cmd/desktop

# Build targets
build: sandbox server desktop

server:
	go build -o bin/sfarm-server ./cmd/server

sandbox:
	cd sandbox-core && cargo build --release
	mkdir -p bin
	cp sandbox-core/target/release/sfarm-sandbox bin/sfarm-sandbox

desktop: sandbox
	cd web && npm install && npm run build
	mkdir -p cmd/desktop/dist
	cp -r web/dist/* cmd/desktop/dist/
	go build -o bin/sfarm-desktop ./cmd/desktop

# Development
dev: db-up
	go run ./cmd/server

# Один сценарий «всё собрать и запустить»: Postgres → ожидание → сборка → sfarm-desktop (встроенный UI + API + sandbox).
# Требуется: Docker, Go, Rust/cargo, Node/npm; для сборки desktop — dev-пакеты X11 (libx11-dev libxtst-dev libxext-dev).
# Первый запуск без кэша: make start-fresh  или  make start FRESH=1
# Порт 8080 по умолчанию слушает все интерфейсы — при необходимости закройте в firewall всё кроме VPN
start: wait-postgres
	@$(MAKE) $(if $(FRESH),rebuild-fresh,build)
	@$(MAKE) run-desktop

start-fresh:
	@$(MAKE) start FRESH=1

wait-postgres: db-up

# Поднять Postgres и дождаться готовности. Без -d steam_farm: на initdb целевая БД может быть ещё не создана.
# compose --wait: Docker Compose v2.20+; иначе — опрос pg_isready -U sfarm (сервер уже принимает соединения).
db-up:
	@set -e; \
	if docker compose up -d --wait --wait-timeout 180 postgres 2>/dev/null; then \
		echo "PostgreSQL is ready."; \
	else \
		echo "Waiting for PostgreSQL (compose without --wait or --wait failed; polling)..."; \
		docker compose up -d postgres; \
		for i in $$(seq 1 180); do \
			if docker compose exec -T postgres pg_isready -U sfarm 2>/dev/null; then \
				echo "PostgreSQL is ready."; \
				exit 0; \
			fi; \
			sleep 1; \
		done; \
		echo "PostgreSQL did not become ready in 180s."; \
		echo "=== docker compose ps -a ==="; \
		docker compose ps -a || true; \
		echo "=== docker compose logs postgres (last 100 lines) ==="; \
		docker compose logs postgres --tail 100 2>&1 || true; \
		echo "=== pg_isready inside container (stderr) ==="; \
		docker compose exec -T postgres pg_isready -U sfarm -v 2>&1 || true; \
		exit 1; \
	fi

run-desktop:
	@test -x ./bin/sfarm-desktop || (echo "Нет bin/sfarm-desktop — сначала: make build"; exit 1)
	@echo "Запуск sfarm-desktop (Ctrl+C — остановить). Откроется браузер с панелью."
	@DATABASE_URL='$(DATABASE_URL)' SFARM_HTTP_LISTEN='$(SFARM_HTTP_LISTEN)' SFARM_HTTP_IFACE_SKIP='$(SFARM_HTTP_IFACE_SKIP)' ./bin/sfarm-desktop

db-down:
	docker compose down

migrate:
	go run ./cmd/server  # migrations run on startup

# Dependencies
deps:
	go mod tidy
	cd web && npm install
	cd sandbox-core && cargo fetch

# Generate protobuf
proto:
	protoc --go_out=. --go-grpc_out=. internal/proto/farm/farm.proto

# Testing
test:
	go test ./... -v

# Linting
lint:
	golangci-lint run ./...

# Clean
clean:
	rm -rf bin/
	rm -rf sandbox-core/target/
	rm -rf web/dist/
	rm -rf cmd/desktop/dist/

# Install tools
tools:
	go install github.com/golang-migrate/migrate/v4/cmd/migrate@latest

help:
	@echo "Основное:"
	@echo "  make start          — поднять Postgres, собрать проект, запустить sfarm-desktop (одна команда до рабочей панели)"
	@echo "  make start-fresh    — то же, но полная пересборка без кэша (как rebuild-fresh)"
	@echo "  make build          — только sandbox + server + desktop (веб в cmd/desktop/dist)"
	@echo "  make rebuild-fresh  — чистая пересборка Go/Rust/npm по правилам проекта"
	@echo "  make dev            — Postgres + go run ./cmd/server (API без встроенного UI)"
	@echo "  make db-up / db-down — только контейнер PostgreSQL"
	@echo "Переменные: DATABASE_URL=...  FRESH=1  SFARM_HTTP_LISTEN (по умолч. 0.0.0.0:8080)  SFARM_HTTP_IFACE_SKIP"
