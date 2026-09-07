PKG     := github.com/mnorrsken/kulprit
BIN     := kubectl-kulprit
NAME    := kulprit
VERSION := $(shell git describe --tags --always --dirty)
COMMIT  := $(shell git rev-parse --short HEAD)
LDFLAGS := -s -w -X $(PKG)/internal/version.Version=$(VERSION) -X $(PKG)/internal/version.Commit=$(COMMIT)

PLATFORMS := darwin/arm64 darwin/amd64 linux/arm64 linux/amd64

# Where "go install" puts things, which is also where the completion helper has
# to go for kubectl to find it.
GOBIN := $(shell go env GOBIN)
ifeq ($(GOBIN),)
GOBIN := $(shell go env GOPATH)/bin
endif

.PHONY: build run test vet fmt install dist clean

build:
	go build -ldflags '$(LDFLAGS)' -o bin/$(BIN) ./cmd/$(BIN)

run:
	go run ./cmd/$(BIN) $(ARGS)

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

# install puts the plugin and its completion helper on $GOBIN (or $GOPATH/bin),
# which is where kubectl looks for both. With that on PATH, "kubectl kulprit"
# works and so does completing it.
install:
	go install -ldflags '$(LDFLAGS)' ./cmd/$(BIN)
	@'$(GOBIN)/$(BIN)' completion

dist:
	@mkdir -p dist
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		echo "dist/$(BIN)-$$os-$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			go build -trimpath -ldflags '$(LDFLAGS)' \
			-o dist/$(BIN)-$$os-$$arch ./cmd/$(BIN) || exit 1; \
	done

clean:
	rm -rf bin dist
