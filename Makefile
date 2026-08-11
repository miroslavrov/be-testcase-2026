.PHONY: up down logs build test tidy migrate

up:
	docker compose up -d --build

down:
	docker compose down

logs:
	docker compose logs -f api worker

build:
	go build ./...

test:
	go test ./... -race -count=1

tidy:
	go mod tidy

migrate:
	go run ./cmd/migrate
