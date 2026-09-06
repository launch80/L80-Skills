BINARY := bin/L80
VERSION ?= 0.2.0
LDFLAGS := -s -w -X github.com/launch80/L80-Skills/internal/cli.Version=$(VERSION)

.PHONY: build install test fmt vet clean hooks

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

# Point git at the committed hooks, which refuse commits and pushes that are
# not authored by the shared launch80 identity (team@launch80.com).
hooks:
	git config core.hooksPath .githooks
	git config user.name launch80
	git config user.email team@launch80.com
	@echo "hooks enabled; commits and pushes must use team@launch80.com"
