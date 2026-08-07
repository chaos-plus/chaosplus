.PHONY: build test lint run coverage docker-build clean

BIN := chaosplus-server
COVER := coverage.out

build:
	go build -trimpath -ldflags="-s -w" -o ./bin/$(BIN) ./cmd/chaosplus-server

test:
	go test -race -count=1 ./...

test-cover:
	go test -race -count=1 -coverprofile=$(COVER) -covermode=atomic ./...
	go tool cover -func=$(COVER)

coverage: test-cover
	go tool cover -html=$(COVER) -o coverage.html

lint:
	go vet ./...

run:
	go run ./cmd/chaosplus-server

run-debug:
	go run ./cmd/chaosplus-server -d

docker-build:
	docker build -t chaosplus:latest .

clean:
	rm -rf ./bin $(COVER) coverage.html
