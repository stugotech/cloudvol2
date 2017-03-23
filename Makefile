PWD=$(shell pwd)
GONAME=$(shell basename "$(PWD)")
GOBIN=$(PWD)/bin
VERSION=$(shell git describe --exact-match 2>/dev/null)
GOOS=linux
GOARCH=amd64
BINFILE=$(GONAME)-$(VERSION)-$(GOOS)-$(GOARCH)

.PHONY: build clean publish _checkversion

build: clean _checkversion
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -v -o bin/$(BINFILE)

publish: build
	gsutil cp -a public-read bin/$(BINFILE) gs://stugo-infrastructure/cloudvol/

clean:
	rm -rf bin

_checkversion:
	@test $(VERSION) || (echo "no version tag found" && false)