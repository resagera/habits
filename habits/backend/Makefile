export GOTOOLCHAIN := auto

DATABASE_URL ?= postgres://postgres:postgres@localhost:5432/streaks?sslmode=disable

.PHONY: run test build psql

run:
	DATABASE_URL='$(DATABASE_URL)' DEV_AUTH_BYPASS=true DEV_USER_ID=1 ADDR=:8081 go run ./cmd/server

test:
	go test ./...

build:
	go build -o bin/server ./cmd/server

psql:
	docker exec -it core-postgres-1 psql -U postgres -d streaks
