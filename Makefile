.PHONY: update-oui update-iana build-linux build-linux-full test test-verbose test-race

test:
	go test -v ./...

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

