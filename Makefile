BUILD_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || printf 'dev')
BUILD_DIRTY := $(shell if [ -n "$(shell git status --porcelain 2>/dev/null)" ]; then printf '%s' '-dirty'; fi)
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -X github.com/robstumborg/conductor/internal/app.version=$(BUILD_COMMIT)$(BUILD_DIRTY) -X github.com/robstumborg/conductor/internal/app.buildCommit=$(BUILD_COMMIT)$(BUILD_DIRTY) -X github.com/robstumborg/conductor/internal/app.buildDate=$(BUILD_DATE)
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
MANDIR ?= $(PREFIX)/share/man
MAN1DIR ?= $(MANDIR)/man1
BIN := conduct
MANPAGE := man/conduct.1

.PHONY: build install uninstall clean test

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/conduct

install: build
	install -d "$(DESTDIR)$(BINDIR)"
	install -d "$(DESTDIR)$(MAN1DIR)"
	install -m 0755 $(BIN) "$(DESTDIR)$(BINDIR)/$(BIN)"
	install -m 0644 $(MANPAGE) "$(DESTDIR)$(MAN1DIR)/conduct.1"

uninstall:
	rm -f "$(DESTDIR)$(BINDIR)/$(BIN)"
	rm -f "$(DESTDIR)$(MAN1DIR)/conduct.1"

clean:
	rm -f $(BIN)

test:
	go test ./...
