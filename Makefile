.PHONY: update-oui update-iana build-linux build-linux-full test test-verbose test-race lint

test:
	go test -v ./...

lint:
	GOCACHE=$$PWD/.gocache GOLANGCI_LINT_CACHE=$$PWD/.cache/golangci-lint golangci-lint run ./...

update-oui:
	go run ./_tools/update_oui

update-iana:
	go run ./_tools/update_iana_numbers

build-linux-local:
	GOOS=linux GOARCH=amd64 go build -o srv-identify-local ./cmd/example/local

build-linux-local-full: update-oui update-iana
	GOOS=linux GOARCH=amd64 go build -o srv-identify-local ./cmd/example/local

build-linux-remote:
	GOOS=linux GOARCH=amd64 go build -o srv-identify-remote ./cmd/example/remote

build-linux-remote-full: update-oui update-iana
	GOOS=linux GOARCH=amd64 go build -o srv-identify-remote ./cmd/example/remote
