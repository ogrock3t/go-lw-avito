.PHONY: generate migrate migrate-up migrate-down migrate-status run test

generate:
	go tool oapi-codegen \
		-generate types,chi-server \
		-package api \
		-o internal/generated/api.gen.go \
		contracts/openapi/trip-service.openapi.yaml

migrate: migrate-up

migrate-up:
	set -a; . ./.env; set +a; go tool goose -dir migrations postgres "$$DATABASE_URL" up

migrate-down:
	set -a; . ./.env; set +a; go tool goose -dir migrations postgres "$$DATABASE_URL" down

migrate-status:
	set -a; . ./.env; set +a; go tool goose -dir migrations postgres "$$DATABASE_URL" status

run:
	set -a; . ./.env; set +a; exec go run ./cmd/trip-service

test:
	go test -race ./...
