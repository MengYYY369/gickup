test:
	go test ./...
.PHONY: test

webui:
	cd webui && npm ci && npm run build
.PHONY: webui

dist:
	mkdir -p dist

dist/gickup: dist webui
	go build -o dist/gickup .

build: dist/gickup
.PHONY: build

clean:
	$(RM) -r dist
.PHONY: clean

install-tools:
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.45.0
.PHONY: install-tools

lint:
	golangci-lint run
.PHONY: lint
