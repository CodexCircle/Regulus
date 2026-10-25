.PHONY: dashboard build test proto proto-check clean

# Build the dashboard into internal/webui/dist so it is embedded in the binary.
dashboard:
	cd dashboard && npm ci && npm run build

build: dashboard
	go build -ldflags "-X main.version=$$(git describe --tags --always --dirty)" -o bin/regulus ./cmd/regulus

test:
	go vet ./...
	go test ./...
	cd dashboard && npm ci && npm run typecheck

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
