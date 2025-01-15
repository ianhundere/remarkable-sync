BINARY=remarkable-sync

.PHONY: build clean

build:
	go build -o $(BINARY) cmd/remarkable-sync/main.go

clean:
	rm -rf $(BINARY)
	go clean
