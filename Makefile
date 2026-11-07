.PHONY: dashboard build lint test test-postgres smoke proto proto-check clean

# Build the dashboard into internal/webui/dist so it is embedded in the binary.
dashboard:
	cd dashboard && npm ci && npm run build

build: dashboard
	go build -ldflags "-X main.version=$$(git describe --tags --always --dirty)" -o bin/regulus ./cmd/regulus

lint:
	golangci-lint run
	cd dashboard && npm ci && npm run lint

test:
	sh scripts/test-install.sh
	sh scripts/test-next-version.sh
	go vet ./...
	go test -race ./...
	cd dashboard && npm ci && npm run typecheck && npm test

# Run the database-dependent tests on PostgreSQL as well. Needs a database you
# can write to, for example:
#   docker run --rm -d -p 5432:5432 -e POSTGRES_USER=regulus -e POSTGRES_PASSWORD=regulus -e POSTGRES_DB=regulus_test postgres:16
#   make test-postgres REGULUS_TEST_DATABASE_URL='postgres://regulus:regulus@localhost:5432/regulus_test?sslmode=disable'
# Each test gets its own schema, dropped when it ends.
test-postgres:
	@test -n "$(REGULUS_TEST_DATABASE_URL)" || { echo 'set REGULUS_TEST_DATABASE_URL (see the comment above this target)'; exit 2; }
	REGULUS_TEST_DATABASE_URL='$(REGULUS_TEST_DATABASE_URL)' go test -race ./internal/storage/... ./internal/auth/... ./internal/fleet/... ./internal/hub/... ./internal/update/... ./cmd/...

# Start the built binary and check it end to end (run make build first).
smoke:
	scripts/smoke.sh bin/regulus

# Regenerate internal/gen from proto/ (generated code is committed, so a normal
# build does not need these tools). Tool versions are pinned here.
TOOLS := $(CURDIR)/bin/tools
proto:
	GOBIN=$(TOOLS) go install github.com/bufbuild/buf/cmd/buf@v1.75.0
	GOBIN=$(TOOLS) go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12
	GOBIN=$(TOOLS) go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2
	PATH=$(TOOLS):$$PATH buf lint proto
	PATH=$(TOOLS):$$PATH buf generate

# Fails if the committed generated code is out of date.
proto-check: proto
	@test -z "$$(git status --porcelain -- internal/gen)" || { echo 'internal/gen is out of date: run make proto and commit'; git status --short -- internal/gen; exit 1; }

clean:
	rm -rf bin internal/webui/dist/* dashboard/node_modules
	touch internal/webui/dist/.gitkeep
