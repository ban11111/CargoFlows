SHELL := /bin/sh

BACKEND_SERVICES := mysql minio migrate api worker
APP_BACKEND_SERVICES := migrate api worker
LAUNCHD_DOMAIN := gui/$(shell id -u)
WEB_LAUNCHD_LABEL := com.cargoflows.web-dev
WEB_LAUNCHD_JOB := $(LAUNCHD_DOMAIN)/$(WEB_LAUNCHD_LABEL)
WEB_LAUNCHD_PLIST := $(CURDIR)/scripts/launchd/$(WEB_LAUNCHD_LABEL).plist
WEB_LOCAL_URL := http://127.0.0.1:3005/login
CLOUDFLARED_JOB := system/com.cloudflare.cloudflared
VALID_SCOPES := all backend frontend
CLI_SCOPE := $(firstword $(filter backend frontend,$(MAKECMDGOALS)))
DEV_SCOPE := $(if $(SCOPE),$(SCOPE),$(if $(CLI_SCOPE),$(CLI_SCOPE),all))
RE_DEV_SCOPE := $(if $(SCOPE),$(SCOPE),$(if $(CLI_SCOPE),$(CLI_SCOPE),backend))

.DEFAULT_GOAL := help

.PHONY: help dev re-dev dev-backend dev-frontend re-dev-backend re-dev-frontend \
	backend-up backend-rebuild frontend-up frontend-rebuild cloudflare-check ps logs \
	frontend-wait logs-frontend down backend frontend

help: ## Show available development commands.
	@echo "CargoFlows development commands"
	@echo
	@echo "  make dev                         Start backend and frontend"
	@echo "  make re-dev                      Rebuild and restart backend only"
	@echo "  make dev backend                 Start backend only"
	@echo "  make dev frontend                Ensure the launchd frontend is running"
	@echo "  make re-dev backend              Rebuild and restart backend only"
	@echo "  make re-dev frontend             Restart the launchd frontend"
	@echo
	@echo "Aliases:"
	@echo "  make dev-backend                 Same as: make dev backend"
	@echo "  make dev-frontend                Same as: make dev frontend"
	@echo "  make re-dev-backend              Same as: make re-dev backend"
	@echo "  make re-dev-frontend             Same as: make re-dev frontend"
	@echo
	@echo "Utilities:"
	@echo "  make ps                          Show backend, frontend, and Tunnel status"
	@echo "  make logs                        Follow backend logs"
	@echo "  make logs-frontend               Follow launchd frontend logs"
	@echo "  make down                        Stop Compose services (not frontend)"
	@echo
	@echo "Options: SCOPE=all|backend|frontend"
	@echo "Defaults: make dev => all; make re-dev => backend"

dev: ## Start all services, or select backend/frontend.
	@case " $(VALID_SCOPES) " in *" $(DEV_SCOPE) "*) ;; *) echo "Invalid SCOPE=$(DEV_SCOPE); use all, backend, or frontend" >&2; exit 2;; esac
	@if [ "$(DEV_SCOPE)" = "all" ] || [ "$(DEV_SCOPE)" = "backend" ]; then $(MAKE) --no-print-directory backend-up; fi
	@if [ "$(DEV_SCOPE)" = "all" ] || [ "$(DEV_SCOPE)" = "frontend" ]; then $(MAKE) --no-print-directory frontend-up; fi
	@if [ "$(DEV_SCOPE)" = "all" ] || [ "$(DEV_SCOPE)" = "frontend" ]; then $(MAKE) --no-print-directory cloudflare-check; fi

re-dev: ## Rebuild application services, or select backend/frontend.
	@case " $(VALID_SCOPES) " in *" $(RE_DEV_SCOPE) "*) ;; *) echo "Invalid SCOPE=$(RE_DEV_SCOPE); use all, backend, or frontend" >&2; exit 2;; esac
	@if [ "$(RE_DEV_SCOPE)" = "all" ] || [ "$(RE_DEV_SCOPE)" = "backend" ]; then $(MAKE) --no-print-directory backend-rebuild; fi
	@if [ "$(RE_DEV_SCOPE)" = "all" ] || [ "$(RE_DEV_SCOPE)" = "frontend" ]; then $(MAKE) --no-print-directory frontend-rebuild; fi
	@if [ "$(RE_DEV_SCOPE)" = "all" ] || [ "$(RE_DEV_SCOPE)" = "frontend" ]; then $(MAKE) --no-print-directory cloudflare-check; fi

backend-up:
	@echo "Starting backend services..."
	docker compose up -d $(BACKEND_SERVICES)

backend-rebuild:
	@echo "Ensuring MySQL and MinIO are running..."
	docker compose up -d mysql minio
	@echo "Rebuilding and recreating migrate, API, and worker..."
	docker compose up -d --build --force-recreate $(APP_BACKEND_SERVICES)

frontend-up:
	@if launchctl print "$(WEB_LAUNCHD_JOB)" >/dev/null 2>&1; then \
		echo "Frontend launchd job is already loaded: $(WEB_LAUNCHD_LABEL)"; \
	else \
		echo "Bootstrapping frontend launchd job..."; \
		launchctl bootstrap "$(LAUNCHD_DOMAIN)" "$(WEB_LAUNCHD_PLIST)"; \
	fi
	@launchctl kickstart "$(WEB_LAUNCHD_JOB)"
	@$(MAKE) --no-print-directory frontend-wait

frontend-rebuild:
	@if launchctl print "$(WEB_LAUNCHD_JOB)" >/dev/null 2>&1; then \
		echo "Restarting frontend launchd job..."; \
		launchctl kickstart -k "$(WEB_LAUNCHD_JOB)"; \
	else \
		$(MAKE) --no-print-directory frontend-up; \
	fi
	@$(MAKE) --no-print-directory frontend-wait
	@echo "Next.js dev will rebuild changed source on demand."

frontend-wait:
	@attempt=0; \
	while [ $$attempt -lt 30 ]; do \
		if curl --silent --show-error --fail --max-time 2 "$(WEB_LOCAL_URL)" >/dev/null 2>&1; then \
			echo "Frontend is ready at http://localhost:3005"; \
			exit 0; \
		fi; \
		attempt=$$((attempt + 1)); \
		sleep 1; \
	done; \
	echo "Frontend did not become ready within 30 seconds; run 'make logs-frontend'." >&2; \
	exit 1

cloudflare-check:
	@if launchctl print "$(CLOUDFLARED_JOB)" >/dev/null 2>&1; then \
		echo "Cloudflare Tunnel daemon is running: https://dev.cargoflows.cc"; \
	else \
		echo "Warning: $(CLOUDFLARED_JOB) is not running; local frontend remains available on port 3005." >&2; \
	fi

dev-backend: backend-up
dev-frontend: frontend-up cloudflare-check
re-dev-backend: backend-rebuild
re-dev-frontend: frontend-rebuild cloudflare-check

ps: ## Show Compose service status.
	docker compose ps -a
	@launchctl print "$(WEB_LAUNCHD_JOB)" >/dev/null 2>&1 && echo "frontend launchd: running" || echo "frontend launchd: not loaded"
	@launchctl print "$(CLOUDFLARED_JOB)" >/dev/null 2>&1 && echo "cloudflared: running" || echo "cloudflared: not running"

logs: ## Follow logs for the backend application services.
	docker compose logs -f api worker migrate

logs-frontend: ## Follow launchd-managed frontend logs.
	tail -n 100 -f tmp/web-launchd.out.log tmp/web-launchd.err.log

down: ## Stop and remove Compose containers while preserving named volumes.
	docker compose down

# Selector goals make both `make dev backend` and `make dev SCOPE=backend` valid.
backend frontend:
	@:
