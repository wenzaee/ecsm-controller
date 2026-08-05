GO ?= go
BIN_DIR ?= bin
GOFLAGS ?= -trimpath

PROGRAMS := ecsm-controller rosedb-dump desired-vsoa-client

ifeq ($(OS),Windows_NT)
	EXE := .exe
else
	EXE :=
endif

.PHONY: build test test-race coverage clean

build:
	@mkdir -p $(BIN_DIR)
	@for program in $(PROGRAMS); do \
		echo "building ./cmd/$$program -> $(BIN_DIR)/$$program$(EXE)"; \
		$(GO) build $(GOFLAGS) -o "$(BIN_DIR)/$$program$(EXE)" "./cmd/$$program"; \
	done
	@echo "build success, output: $(BIN_DIR)"

# test 运行全部后端单元测试。CI 会额外保存 JSON 和覆盖率报告。
test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

coverage:
	@mkdir -p artifacts
	$(GO) test -coverprofile=artifacts/coverage.out ./...
	$(GO) tool cover -func=artifacts/coverage.out
	$(GO) tool cover -html=artifacts/coverage.out -o artifacts/coverage.html

clean:
	rm -rf $(BIN_DIR)
