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

.PHONY: help tools windows mac install uninstall run test
