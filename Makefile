PKG     := github.com/mnorrsken/kulprit
BIN     := kubectl-kulprit
VERSION := $(shell git describe --tags --always --dirty)
COMMIT  := $(shell git rev-parse --short HEAD)
LDFLAGS := -s -w -X $(PKG)/internal/version.Version=$(VERSION) -X $(PKG)/internal/version.Commit=$(COMMIT)

PLATFORMS := darwin/arm64 darwin/amd64 linux/arm64 linux/amd64

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

# install puts the plugin on $GOBIN (or $GOPATH/bin), which is where kubectl
# looks for it. With that on PATH, "kubectl kulprit" works.
install:
	go install -ldflags '$(LDFLAGS)' ./cmd/$(BIN)

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
