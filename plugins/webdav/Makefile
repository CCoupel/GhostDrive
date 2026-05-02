# GhostDrive WebDAV Plugin — Makefile
# Builds the ghostdrive-webdav standalone gRPC binary.
#
# The binary entry point lives in plugins/webdav/cmd/ (package main) and
# imports the plugin implementation from plugins/webdav/ (package webdav).
# This mirrors the echo plugin layout (sdk/go/echo/) and avoids the
# package-name conflict that would arise from a package main file in the
# same directory as the package webdav library files.
#
# Usage (run from the repository root):
#   make -f plugins/webdav/Makefile build        — compile ghostdrive-webdav.exe (Windows AMD64)
#   make -f plugins/webdav/Makefile build-linux  — compile ghostdrive-webdav     (Linux AMD64)
#   make -f plugins/webdav/Makefile build-all    — compile both targets
#   make -f plugins/webdav/Makefile clean        — remove all build artefacts

GOARCH := amd64
CGO    := 0

.PHONY: build build-linux build-all clean

build:
	@echo "Building ghostdrive-webdav plugin for windows/$(GOARCH)..."
	GOOS=windows GOARCH=$(GOARCH) CGO_ENABLED=$(CGO) \
		go build -ldflags="-s -w" -o ghostdrive-webdav.exe ./plugins/webdav/cmd/
	@echo "Done: ghostdrive-webdav.exe"

build-linux:
	@echo "Building ghostdrive-webdav plugin for linux/$(GOARCH)..."
	GOOS=linux GOARCH=$(GOARCH) CGO_ENABLED=$(CGO) \
		go build -ldflags="-s -w" -o ghostdrive-webdav ./plugins/webdav/cmd/
	@echo "Done: ghostdrive-webdav"

build-all: build build-linux
	@echo "All targets built."

clean:
	@rm -f ghostdrive-webdav.exe ghostdrive-webdav
	@echo "Cleaned."
