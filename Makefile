.PHONY: dev ui build docker clean

# terminal 1: make dev   (Go API on :8090)   terminal 2: cd ui && npm run dev (Vite on :5173, proxies /api)
dev:
	go run . serve --http=127.0.0.1:8090 --dir=./pb_data

ui:
	cd ui && npm ci && npm run build

build: ui
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o murmelmoney .

docker:
	docker build -t murmelmoney .

clean:
	rm -rf murmelmoney ui/dist
