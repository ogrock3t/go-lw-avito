.PHONY: generate migrate migrate-up migrate-down migrate-status run test test-integration lint

generate:
	go tool oapi-codegen \
		-generate types,chi-server \
		-package api \
		-o internal/generated/api.gen.go \
		contracts/openapi/trip-service.openapi.yaml

	go generate ./...

migrate: migrate-up

migrate-up:
	set -a; . ./.env; set +a; go tool goose -dir migrations postgres "$$DATABASE_URL" up

migrate-down:
	set -a; . ./.env; set +a; go tool goose -dir migrations postgres "$$DATABASE_URL" down

migrate-status:
	set -a; . ./.env; set +a; go tool goose -dir migrations postgres "$$DATABASE_URL" status

run:
	set -a; . ./.env.example; . ./.env; set +a; exec go run ./cmd/trip-service

test:
	go test -race -covermode=atomic -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -n 1

test-integration:
	set -a; . ./.env; set +a; go test -race -count=2 -tags=integration ./internal/repository/trip/... ./internal/infra/postgres/...

lint:
	go tool golangci-lint run ./...
