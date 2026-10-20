.PHONY: frontend test build arm64 run dev-web

frontend/node_modules/.installed: frontend/package.json frontend/package-lock.json
	cd frontend && npm ci --no-audit --no-fund
	touch frontend/node_modules/.installed

frontend: frontend/node_modules/.installed
	npm --prefix frontend run build

test: frontend
	npm --prefix frontend test
	go test ./...

build: frontend
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/proto-watch ./cmd/proto-watch

arm64: frontend
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o bin/proto-watch-linux-arm64 ./cmd/proto-watch

run: frontend
	go run ./cmd/proto-watch

dev-web: frontend/node_modules/.installed
	npm --prefix frontend run dev
