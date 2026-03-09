.PHONY: update-oui update-iana build-linux build-linux-full

update-oui:
	go run ./_tools/update_oui

update-iana:
	go run ./_tools/update_iana_numbers

build-linux:
	GOOS=linux GOARCH=amd64 go build -o server-identify .

build-linux-full: update-oui update-iana
	GOOS=linux GOARCH=amd64 go build -o server-identify .