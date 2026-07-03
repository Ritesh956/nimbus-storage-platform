.PHONY: dev down build test lint

dev:
	docker compose -f deploy/docker-compose.yml up --build

down:
	docker compose -f deploy/docker-compose.yml down -v

build:
	cd backend && go build ./...

test:
	cd backend && go test ./...

lint:
	cd backend && go vet ./...
