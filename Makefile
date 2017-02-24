PWD=$(shell pwd)
GONAME=$(shell basename "$(PWD)")
GOBIN=$(PWD)/bin

.PHONY: build

build: 
	GOOS=linux GOARCH=amd64 go build -v -o bin/$(GONAME)