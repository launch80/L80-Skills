BINARY := bin/L80
VERSION ?= 0.1.0
LDFLAGS := -s -w -X github.com/mgeatz/L80-Skills/internal/cli.Version=$(VERSION)

.PHONY: build install test fmt vet clean

# rm first: on a case-insensitive filesystem (default macOS) an existing
# bin/l80 keeps its old directory entry when overwritten, so the release
# tarball would ship a lowercase name that Linux then fails to find.
build:
	@rm -f $(BINARY) bin/l80
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/L80

install: build
	mkdir -p $(HOME)/.local/bin
	cp $(BINARY) $(HOME)/.local/bin/L80
	@echo "installed to $(HOME)/.local/bin/L80"

test:
	go test ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

clean:
	rm -rf bin
