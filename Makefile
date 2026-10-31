VERSION ?= 0.0.0-dev
COMMIT ?= $(shell git rev-parse HEAD)
FLAVOR ?= dev
LDFLAGS = -X clubmate/internal/buildinfo.Version=$(VERSION) -X clubmate/internal/buildinfo.Commit=$(COMMIT) -X clubmate/internal/buildinfo.Flavor=$(FLAVOR)
MACOS_MIN = 27.0
GO_ENV = MACOSX_DEPLOYMENT_TARGET=$(MACOS_MIN) CGO_CFLAGS="-O2 -g -mmacosx-version-min=$(MACOS_MIN)" CGO_LDFLAGS="-O2 -g -mmacosx-version-min=$(MACOS_MIN)"
GO_BUILD = $(GO_ENV) go build -ldflags "$(LDFLAGS)"
DEV_SIGN_IDENTITY ?= $(shell security find-identity -p codesigning -v 2>/dev/null | awk '/"Apple Development: / {print $$2; exit}')

build:
	rm -rf bin/plugins
	$(GO_BUILD) -o bin/ ./cmd/...
	$(GO_BUILD) -o bin/plugins/terminals/terminals ./plugins/terminals
	cp plugins/terminals/plugin.json bin/plugins/terminals/
	$(GO_BUILD) -o bin/plugins/editor-nvim/editor-nvim ./plugins/editor-nvim
	cp plugins/editor-nvim/plugin.json bin/plugins/editor-nvim/
	$(GO_BUILD) -o bin/plugins/git/git ./plugins/git
	cp plugins/git/plugin.json bin/plugins/git/
	$(GO_BUILD) -o bin/plugins/processes/processes ./plugins/processes
	cp plugins/processes/plugin.json bin/plugins/processes/
	$(GO_BUILD) -o bin/plugins/search/search ./plugins/search
	cp plugins/search/plugin.json bin/plugins/search/
	$(GO_BUILD) -o bin/plugins/agents/agents ./plugins/agents
	cp plugins/agents/plugin.json bin/plugins/agents/
	$(GO_BUILD) -o bin/plugins/remote/remote ./plugins/remote
	cp plugins/remote/plugin.json bin/plugins/remote/
	$(GO_BUILD) -o bin/plugins/cron/cron ./plugins/cron
	cp plugins/cron/plugin.json bin/plugins/cron/
	$(GO_BUILD) -o bin/plugins/triggers/triggers ./plugins/triggers
	cp plugins/triggers/plugin.json bin/plugins/triggers/
	$(GO_BUILD) -o bin/plugins/backlog/backlog ./plugins/backlog
	cp plugins/backlog/plugin.json bin/plugins/backlog/
	$(GO_BUILD) -o bin/plugins/graphs/graphs ./plugins/graphs
	cp plugins/graphs/plugin.json bin/plugins/graphs/

run-core: build
	bin/clubmate-core

test:
	go test ./...

lint:
	go vet ./...

ghostty:
	scripts/build-ghostty.sh

app: ghostty
	cd client && xcodegen generate && xcodebuild -project Clubmate.xcodeproj -scheme Clubmate -configuration Debug -derivedDataPath ../build/DerivedData MARKETING_VERSION=$(VERSION) CLUBMATE_COMMIT=$(COMMIT) $(if $(DEV_SIGN_IDENTITY),CODE_SIGN_IDENTITY=$(DEV_SIGN_IDENTITY)) build

app-release: ghostty
	cd client && xcodegen generate && xcodebuild -project Clubmate.xcodeproj -scheme Clubmate -configuration Release -derivedDataPath ../build/DerivedData MARKETING_VERSION=$(VERSION) CLUBMATE_COMMIT=$(COMMIT) CLUBMATE_FLAVOR=release build

run-app:
	open "build/DerivedData/Build/Products/Debug/Clubmate Dev.app"

kit-test:
	cd client/ClubmateKit && swift test

nvim-distro:
	scripts/vendor-tools.sh build/resources
	scripts/vendor-nvim-distro.sh build/resources

nvim-test: nvim-distro
	CLUBMATE_TEST_RESOURCES=$(CURDIR)/build/resources go test ./nvim-distro/...

bundle-smoke:
	$(GO_ENV) CLUBMATE_TEST_RESOURCES="$(RESOURCES)" CLUBMATE_TEST_BUNDLE=1 go test -count=1 -run '^TestBundledAppSmoke$$' ./nvim-distro/

companion-kit-test:
	cd client/CompanionKit && swift test

companion:
	cd client && xcodegen generate --spec companion.yml && xcodebuild -project Companion.xcodeproj -scheme Companion -sdk iphonesimulator -configuration Debug -derivedDataPath ../build/CompanionDerivedData CODE_SIGN_IDENTITY=- CODE_SIGN_STYLE=Manual build

.PHONY: build run-core test lint ghostty app app-release run-app kit-test nvim-distro nvim-test bundle-smoke companion-kit-test companion
