PREFIX ?= /usr
DESTDIR ?=
BINDIR ?= $(PREFIX)/bin

all: oberon

oberon:
	go build -trimpath -o $@ .

install: oberon
	install -d "$(DESTDIR)$(BINDIR)"
	install -m 0755 oberon "$(DESTDIR)$(BINDIR)/oberon"

test:
	go test ./...

clean:
	rm -f oberon

.PHONY: all install test clean
