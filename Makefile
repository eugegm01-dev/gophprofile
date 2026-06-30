.PHONY: help dev build test lint clean run-server run-worker

help: ## Показать справку
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

dev: ## Запустить всю среду (инфра + приложения)
	docker compose -f docker/docker-compose.yml up -d
	docker compose -f docker/docker-compose.app.yml up -d --build

dev-infra: ## Запустить только инфраструктуру
	docker compose -f docker/docker-compose.yml up -d

stop: ## Остановить всё
	docker compose -f docker/docker-compose.app.yml down
	docker compose -f docker/docker-compose.yml down

build: ## Собрать бинарники
	go build -o bin/server ./cmd/server
	go build -o bin/worker ./cmd/worker

test: ## Запустить тесты
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

lint: ## Запустить линтер
	golangci-lint run ./...

clean: ## Очистить
	rm -rf bin/ coverage.out coverage.html

run-server: ## Запустить сервер
	go run ./cmd/server

run-worker: ## Запустить worker
	go run ./cmd/worker

tidy: ## Очистить зависимости
	go mod tidy
	go mod verify

webhook: ## Запустить webhook для приёма алертов
	go run scripts/alert_webhook.go