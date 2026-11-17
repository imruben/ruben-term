# ruben-term · `make` alone lists the targets
WAILS ?= $(shell command -v wails || echo $(HOME)/go/bin/wails)
FLAGS  = -skipbindings -m -nosyncgomod

help:
	@echo "make windows    build build/bin/ruben-term.exe (works from WSL, Linux or Windows)"
	@echo "make mac        build build/bin/ruben-term.app (on a Mac)"
	@echo "make install    WSL: build, copy the .exe to Windows and add it to the Start menu"
	@echo "make uninstall  WSL: remove what install put in Windows"
	@echo "make run        WSL: build and start it"
	@echo "make test       go vet + the tests"
	@echo "make tools      install the wails command"
	@echo "make release V=1.2.3   tag v1.2.3 and push it: CI publishes the installers"

tools:
	go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.1

windows:
	$(WAILS) build -platform windows/amd64 $(FLAGS)

mac:
	$(WAILS) build -platform darwin/universal $(FLAGS)

install: windows
	@sh scripts/install-windows.sh

uninstall:
	@sh scripts/install-windows.sh remove

run: windows
	@./build/bin/ruben-term.exe >/dev/null 2>&1 &

test:
	go vet ./...
	GOOS=windows go vet ./...
	go test ./...

# The tag starts .github/workflows/build.yml, which builds the installer and publishes
# it as a GitHub release; installed apps then offer to update.
release:
	@echo "$(V)" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$$' || { echo "usage: make release V=1.2.3"; exit 1; }
	@git diff --quiet HEAD || { echo "commit your changes first"; exit 1; }
	git tag -a v$(V) -m "ruben-term $(V)"
	git push origin main v$(V)

.PHONY: help tools windows mac install uninstall run test release
