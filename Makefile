GO ?= go
BIN_DIR ?= bin
GOFLAGS ?= -trimpath

PROGRAMS := ecsm-controller rosedb-dump desired-vsoa-client

ifeq ($(OS),Windows_NT)
	EXE := .exe
else
	EXE :=
endif

.PHONY: build clean

build:
	@mkdir -p $(BIN_DIR)
	@for program in $(PROGRAMS); do \
		echo "building ./cmd/$$program -> $(BIN_DIR)/$$program$(EXE)"; \
		$(GO) build $(GOFLAGS) -o "$(BIN_DIR)/$$program$(EXE)" "./cmd/$$program"; \
	done
	@echo "build success, output: $(BIN_DIR)"

clean:
	rm -rf $(BIN_DIR)
