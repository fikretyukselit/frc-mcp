# frc-mcp — developer entry points. CI runs the same targets.
GO       ?= go
BIN      := bin/frc-mcp
SHARDS   := .shards
PKGS     := ./...
export CGO_ENABLED := 0

.PHONY: all build test race bench lint vuln fuzz fixture serve doctor golden clean

all: lint test build

build: ## static binary (CGO disabled)
	$(GO) build -trimpath -ldflags "-s -w" -o $(BIN) ./cmd/frc-mcp

test: ## unit + integration tests
	$(GO) test -shuffle=on $(PKGS)

race: ## tests with the race detector (needs cgo)
	CGO_ENABLED=1 $(GO) test -race -shuffle=on $(PKGS)

bench: ## hot-path benchmarks; compare runs with benchstat
	$(GO) test -run='^$$' -bench=. -benchmem -count=6 ./internal/... | tee bench.txt

lint:
	$(GO) vet $(PKGS)
	test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)
	golangci-lint run

vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@latest $(PKGS)

fuzz: ## short fuzzing pass over parsers and normalizers
	$(GO) test ./internal/textutil -run='^$$' -fuzz=FuzzSnippet -fuzztime=30s
	$(GO) test ./internal/textutil -run='^$$' -fuzz=FuzzFTSQuery -fuzztime=30s
	$(GO) test ./internal/ingest/sanitize -run='^$$' -fuzz=FuzzCleanIdempotent -fuzztime=30s

golden: ## regenerate golden files (review the diff!)
	$(GO) test ./internal/render ./internal/mcpserver -update

fixture: build ## build the synthetic fixture shard
	$(BIN) index build --chunks testdata/fixture/chunks.jsonl --symbols testdata/fixture/symbols.jsonl --out $(SHARDS)/fixture.sqlite

serve: fixture ## run the MCP server over stdio on the fixture shard
	$(BIN) serve --index $(SHARDS)

doctor: fixture
	$(BIN) doctor --index $(SHARDS)

clean:
	rm -rf bin dist $(SHARDS) bench.txt
