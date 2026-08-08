.PHONY: dev down build test test-cover test-integration lint openapi

dev:
	docker compose -f deploy/docker-compose.yml up --build

down:
	docker compose -f deploy/docker-compose.yml down -v

build:
	cd backend && go build ./...

test:
	cd backend && go test ./...

# Coverage across every package, not just the ones with _test.go files
# (-coverpkg=./...) — without it a package with zero tests silently reports
# no line rather than 0%, which is exactly the number audit §14 flagged as
# missing. Run against a running Compose stack for the integration tag to
# actually exercise anything beyond unit tests.
test-cover:
	cd backend && go test ./... -coverprofile=/tmp/nimbus-unit.cov -coverpkg=./... && go tool cover -func=/tmp/nimbus-unit.cov | tail -1

# Requires `docker compose -f deploy/docker-compose.yml up` running first —
# same NIMBUS_TEST_*_DSN/ADDR/ENDPOINT env vars CI sets, pointed at the local
# stack's default ports instead of CI's service containers.
test-integration:
	cd backend && NIMBUS_TEST_POSTGRES_DSN="postgres://nimbus:nimbus@localhost:5432/nimbus?sslmode=disable" \
		NIMBUS_TEST_REDIS_ADDR="localhost:6379" \
		NIMBUS_TEST_MINIO_ENDPOINT="localhost:9000" \
		NIMBUS_TEST_MINIO_ACCESS_KEY="nimbus" \
		NIMBUS_TEST_MINIO_SECRET_KEY="nimbus-secret" \
		NIMBUS_TEST_NATS_URL="nats://localhost:4222" \
		go test -tags=integration ./...

lint:
	cd backend && go vet ./...

# Regenerates docs/openapi.json from cmd/api/*.go's real route registrations,
# then the frontend's generated types from that spec (roadmap #11) — CI
# fails if either is stale, so run this after touching a route or an
# internal/apidoc DTO.
openapi:
	cd backend && go run ./cmd/openapi-gen -out ../docs/openapi.json
	cd frontend && npx openapi-typescript ../docs/openapi.json -o lib/api-schema.generated.ts
