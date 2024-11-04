VERSION := 1.7.0
BINNAME := solbuild

.PHONY: build
build:
	go build -ldflags "-X github.com/getsolus/solbuild/util.SolbuildVersion=$(VERSION)" -o bin/$(BINNAME) $(CURDIR)/main.go

.PHONY: install
install:
	test -d $(DESTDIR)/usr/bin || install -Ddm 00755 $(DESTDIR)/usr/bin
	install -m 00755 bin/* $(DESTDIR)/usr/bin/.
	test -d $(DESTDIR)/usr/share/solbuild || install -Ddm 00755 $(DESTDIR)/usr/share/solbuild
	install -m 00644 data/*.profile $(DESTDIR)/usr/share/solbuild/.
	install -m 00644 data/00_solbuild.conf $(DESTDIR)/usr/share/solbuild/.
	test -d $(DESTDIR)/usr/share/man/man1 || install -Ddm 00755 $(DESTDIR)/usr/share/man/man1
	install -m 00644 man/*.1 $(DESTDIR)/usr/share/man/man1/.
	test -d $(DESTDIR)/usr/share/man/man5 || install -Ddm 00755 $(DESTDIR)/usr/share/man/man5
	install -m 00644 man/*.5 $(DESTDIR)/usr/share/man/man5/.
	test -d $(DESTDIR)/usr/share/bash-completion/completions/ || install -Ddm 00755 $(DESTDIR)/usr/share/bash-completion/completions/
	install -m 00644 data/completions.bash $(DESTDIR)/usr/share/bash-completion/completions/solbuild
.PHONY: check
check:
	go test ./...

.PHONY: spellcheck
spellcheck:
	misspell -error -i 'evolveos' $(shell find $(CURDIR) -name '*.go')

.PHONY: compliant
compliant: spellcheck
	go fmt ./...
	golint ./...
	go vet ./...

# Credit to swupd developers: https://github.com/clearlinux/swupd-client
MANPAGES := \
	man/solbuild.1 \
	man/solbuild.conf.5 \
	man/solbuild.profile.5

.PHONY: gen_docs
gen_docs:
	for MANPAGE in $(MANPAGES); do \
		ronn --roff < $${MANPAGE}.md > $${MANPAGE}; \
		ronn --html < $${MANPAGE}.md > $${MANPAGE}.html; \
	done

# See: https://github.com/meitar/git-archive-all.sh/blob/master/git-archive-all.sh
.PHONY: release
release:
	git-archive-all --format tar --prefix solbuild-$(VERSION)/ --verbose -t HEAD solbuild-$(VERSION).tar
	xz -9 "solbuild-${VERSION}.tar"

.PHONY: clean
clean:
	rm -rf $(CURDIR)/bin
