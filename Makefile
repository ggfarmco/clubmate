VERSION ?= 0.0.0-dev
COMMIT ?= $(shell git rev-parse HEAD)
FLAVOR ?= dev
LDFLAGS = -X clubmate/internal/buildinfo.Version=$(VERSION) -X clubmate/internal/buildinfo.Commit=$(COMMIT) -X clubmate/internal/buildinfo.Flavor=$(FLAVOR)
MACOS_MIN = 27.0
GO_ENV = MACOSX_DEPLOYMENT_TARGET=$(MACOS_MIN) CGO_CFLAGS="-O2 -g -mmacosx-version-min=$(MACOS_MIN)" CGO_LDFLAGS="-O2 -g -mmacosx-version-min=$(MACOS_MIN)"
GO_BUILD = $(GO_ENV) go build -ldflags "$(LDFLAGS)"

build:
	rm -rf bin/plugins
	$(GO_BUILD) -o bin/ ./cmd/...
	$(GO_BUILD) -o bin/plugins/terminals/terminals ./plugins/terminals
	cp plugins/terminals/plugin.json bin/plugins/terminals/
	$(GO_BUILD) -o bin/plugins/editor-nvim/editor-nvim ./plugins/editor-nvim
	cp plugins/editor-nvim/plugin.json bin/plugins/editor-nvim/
	$(GO_BUILD) -o bin/plugins/files/files ./plugins/files
	cp plugins/files/plugin.json bin/plugins/files/
	$(GO_BUILD) -o bin/plugins/git/git ./plugins/git
	cp plugins/git/plugin.json bin/plugins/git/
	$(GO_BUILD) -o bin/plugins/processes/processes ./plugins/processes
	cp plugins/processes/plugin.json bin/plugins/processes/

run-core: build
	bin/clubmate-core

test:
	go test ./...

lint:
	go vet ./...

ghostty:
	scripts/build-ghostty.sh

app: ghostty
	cd client && xcodegen generate && xcodebuild -project Clubmate.xcodeproj -scheme Clubmate -configuration Debug -derivedDataPath ../build/DerivedData MARKETING_VERSION=$(VERSION) CLUBMATE_COMMIT=$(COMMIT) build

app-release: ghostty
	cd client && xcodegen generate && xcodebuild -project Clubmate.xcodeproj -scheme Clubmate -configuration Release -derivedDataPath ../build/DerivedData MARKETING_VERSION=$(VERSION) CLUBMATE_COMMIT=$(COMMIT) CLUBMATE_FLAVOR=release build

run-app:
	open "build/DerivedData/Build/Products/Debug/Clubmate Dev.app"

kit-test:
	cd client/ClubmateKit && swift test

.PHONY: build run-core test lint ghostty app app-release run-app kit-test
