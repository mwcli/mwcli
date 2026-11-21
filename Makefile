VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//')
DATE ?= $(shell date -u +%Y-%m-%d)
LDFLAGS := -X github.com/mwcli/mwcli/internal/build.Version=$(VERSION) -X github.com/mwcli/mwcli/internal/build.Date=$(DATE)

prefix ?= /usr/local
bindir ?= $(prefix)/bin

.PHONY: bin/mw build test lint install uninstall clean

build: bin/mw

bin/mw:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $@ ./cmd/mw

test:
	go test ./...

lint:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }
	go vet ./...

install: bin/mw
	install -d $(DESTDIR)$(bindir)
	install -m755 bin/mw $(DESTDIR)$(bindir)/mw

uninstall:
	rm -f $(DESTDIR)$(bindir)/mw

clean:
	rm -rf bin
