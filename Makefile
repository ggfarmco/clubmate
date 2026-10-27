build:
	go build -o bin/ ./cmd/...
	go build -o bin/plugins/terminals/terminals ./plugins/terminals
	cp plugins/terminals/plugin.json bin/plugins/terminals/

run-core: build
	bin/clubmate-core

test:
	go test ./...

lint:
	go vet ./...

ghostty:
	scripts/build-ghostty.sh

app: ghostty
	cd client && xcodegen generate && xcodebuild -project Clubmate.xcodeproj -scheme Clubmate -configuration Debug -derivedDataPath ../build/DerivedData build

run-app:
	open build/DerivedData/Build/Products/Debug/Clubmate.app

kit-test:
	cd client/ClubmateKit && swift test

.PHONY: build run-core test lint ghostty app run-app kit-test
