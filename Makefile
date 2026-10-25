.PHONY: dashboard build lint test clean

# Build the dashboard into internal/webui/dist so it is embedded in the binary.
dashboard:
	cd dashboard && npm ci && npm run build

build: dashboard
	go build -ldflags "-X main.version=$$(git describe --tags --always --dirty)" -o bin/regulus ./cmd/regulus

lint:
	golangci-lint run
	cd dashboard && npm ci && npm run lint

test:
	go vet ./...
	go test -race ./...
	cd dashboard && npm ci && npm run typecheck && npm test

clean:
	rm -rf bin internal/webui/dist/* dashboard/node_modules
	touch internal/webui/dist/.gitkeep
