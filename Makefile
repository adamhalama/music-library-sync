.PHONY: install uninstall
PREFIX ?= $(shell brew --prefix 2>/dev/null || echo /usr/local)
BINDIR := $(PREFIX)/bin
SCRIPT := $(abspath $(CURDIR)/bin/update-downloads)

install:
	@mkdir -p "$(BINDIR)"
	@ln -sfn "$(SCRIPT)" "$(BINDIR)/update-downloads"
	@echo "Installed -> $(BINDIR)/update-downloads -> $(SCRIPT)"

uninstall:
	@rm -f "$(BINDIR)/update-downloads"
	@echo "Removed $(BINDIR)/update-downloads"
