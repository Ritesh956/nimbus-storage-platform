.PHONY: dev down build test lint openapi

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

# Regenerates docs/openapi.json from cmd/api/*.go's real route registrations,
# then the frontend's generated types from that spec (roadmap #11) — CI
# fails if either is stale, so run this after touching a route or an
# internal/apidoc DTO.
openapi:
	cd backend && go run ./cmd/openapi-gen -out ../docs/openapi.json
	cd frontend && npx openapi-typescript ../docs/openapi.json -o lib/api-schema.generated.ts
