.PHONY: build test race clean install docs-serve

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/saw ./cmd/saw

test:
	go test ./... -count=1

race:
	go test ./... -count=1 -race

clean:
	rm -rf bin dist

install: build
	install -m 755 bin/saw "$(HOME)/.local/bin/saw"

# Local preview for GitHub Pages site (docs/)
docs-serve:
	@echo "SAW site → http://127.0.0.1:4173/"
	python3 -m http.server 4173 --directory docs
