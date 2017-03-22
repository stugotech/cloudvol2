PWD=$(shell pwd)
GONAME=$(shell basename "$(PWD)")
GOBIN=$(PWD)/bin
VERSION=$(shell git describe)
GOOS=linux
GOARCH=amd64
BINFILE=$(GONAME)-$(VERSION)-$(GOOS)-$(GOARCH)

.PHONY: build clean publish

build: 
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -v -o bin/$(BINFILE)

publish: 
	gsutil cp -a public-read bin/$(BINFILE) gs://stugo-infrastructure/cloudvol/

clean:
	rm -rf bin