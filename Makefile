BINARY := jack-ketch
BIN_DIR := bin
PREFIX ?= $(HOME)/.local

.PHONY: build test race vet fmt tidy clean install snapshot proto gen-deps ui ui-web ui-web-embed ui-check ci-check plugin-sync

build:
	@if [ -d ui/dist-web ]; then $(MAKE) ui-web-embed; fi
	go build -o $(BIN_DIR)/$(BINARY) ./cmd/jack-ketch
	go build -o $(BIN_DIR)/jackketchd ./cmd/jackketchd
	go build -o $(BIN_DIR)/jack-ketch-shim ./cmd/jack-ketch-shim

# ui builds the web frontend (embedded by jackketchd in internal/webui/dist).
ui:
	cd ui && npm install --no-audit --no-fund && npm run build

ui-web:
	cd ui && npm run build
	$(MAKE) ui-web-embed

ui-web-embed:
	@if [ -d ui/dist-web ]; then \
		rm -rf internal/webui/dist; \
		mkdir -p internal/webui/dist; \
		cp -r ui/dist-web/. internal/webui/dist/; \
		printf '{"rev":"%s","dirty":%s,"built":"%s"}\n' "$$(git rev-parse --short HEAD 2>/dev/null || echo unknown)" "$$(git diff --quiet 2>/dev/null && echo false || echo true)" "$$(date -u +%Y-%m-%dT%H:%M:%SZ)" > internal/webui/dist/.build.json; \
	fi

plugin-sync:
	rm -rf packaging/claude-plugin/skills && mkdir -p packaging/claude-plugin/skills && cp -r internal/integrate/skill/. packaging/claude-plugin/skills/

ui-check:
	cd ui && npm run check && npm test && npm run build

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

tidy:
	go mod tidy

clean:
	rm -rf $(BIN_DIR)
	rm -rf gen/
	rm -rf ui/src/gen/
	rm -rf ui/dist ui/dist-web

install:
	go install ./cmd/jack-ketch
	go install ./cmd/jackketchd
	go install ./cmd/jack-ketch-shim

snapshot:
	goreleaser release --snapshot --clean

proto:
	PATH="$(PWD)/ui/node_modules/.bin:$$(go env GOPATH)/bin:$$PATH" buf generate
	buf lint
	buf breaking --against '.git#branch=master'
	@echo "Protobuf generated files are in gen/"

gen-deps:
	go install github.com/bufbuild/buf/cmd/buf@v1.50.0
	go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.6
	go install connectrpc.com/connect/cmd/protoc-gen-connect-go@v1.18.1
	@echo "Protobuf tooling installed"

ci-check: build vet race test proto ui-check
	@echo "All checks passed"
