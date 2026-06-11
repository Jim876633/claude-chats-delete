BINARY   := c
INSTALL  := $(HOME)/.local/bin

build:
	go build -o $(BINARY) -ldflags="-s -w" .

install: build
	mkdir -p $(INSTALL)
	mv $(BINARY) $(INSTALL)/$(BINARY)
	@echo "Installed: $(INSTALL)/$(BINARY)"
	@echo "Run: $(BINARY)"

uninstall:
	rm -f $(INSTALL)/$(BINARY)
	@echo "Removed: $(INSTALL)/$(BINARY)"

.PHONY: build install uninstall
