# GhostDrive MooseFS Plugin — Makefile
# Builds the ghostdrive-moosefs standalone gRPC binary.
#
# The binary entry point lives in plugins/moosefs/cmd/ (package main) and
# imports the plugin implementation from plugins/moosefs/ (package moosefs).
# This mirrors the WebDAV plugin layout (plugins/webdav/) and avoids the
# package-name conflict that would arise from a package main file in the
# same directory as the package moosefs library files.
#
# Output extension: .ghdp (GhostDrive Plugin) on all platforms — the loader
# discovers binaries by scanning for *.ghdp files adjacent to the main binary.
#
# Usage (run from the repository root):
#   make -f plugins/moosefs/Makefile build        — compile ghostdrive-moosefs.ghdp (Windows AMD64)
#   make -f plugins/moosefs/Makefile build-linux  — compile ghostdrive-moosefs.ghdp (Linux AMD64)
#   make -f plugins/moosefs/Makefile build-all    — compile both targets
#   make -f plugins/moosefs/Makefile clean        — remove all build artefacts

GOARCH := amd64
CGO    := 0

.PHONY: build build-linux build-all clean

build:
	@echo "Building ghostdrive-moosefs plugin for windows/$(GOARCH)..."
	GOOS=windows GOARCH=$(GOARCH) CGO_ENABLED=$(CGO) \
		go build -ldflags="-s -w" -o ghostdrive-moosefs.ghdp ./plugins/moosefs/cmd/
	@echo "Done: ghostdrive-moosefs.ghdp"

build-linux:
	@echo "Building ghostdrive-moosefs plugin for linux/$(GOARCH)..."
	GOOS=linux GOARCH=$(GOARCH) CGO_ENABLED=$(CGO) \
		go build -ldflags="-s -w" -o ghostdrive-moosefs.ghdp ./plugins/moosefs/cmd/
	@echo "Done: ghostdrive-moosefs.ghdp"

build-all: build build-linux
	@echo "All targets built."

clean:
	@rm -f ghostdrive-moosefs.ghdp
	@echo "Cleaned."
