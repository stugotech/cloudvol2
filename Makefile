PWD=$(shell pwd)
GONAME=$(shell basename "$(PWD)")
GOBIN=$(PWD)/bin
VERSION=$(shell git describe)
GOOS=linux
GOARCH=amd64

.PHONY: build clean

build: 
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -v -o bin/$(GONAME)-$(VERSION)-$(GOOS)-$(GOARCH)

publish: 
	gsutil cp -a public_read bin/$(GONAME)-$(VERSION)-$(GOOS)-$(GOARCH) gs://stugo-infrastructure/cloudvol/

clean:
	rm -rf bin