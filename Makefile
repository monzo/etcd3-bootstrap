
SHELL := /bin/bash
S3_BUCKET := "monzo-deployment-artifacts"
VERSION := "v1.0.2"

.DEFAULT_GOAL: etcd3-bootstrap-linux-amd64-$(VERSION)

etcd3-bootstrap-linux-amd64-$(VERSION): vendor/ *.go
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o etcd3-bootstrap-linux-amd64-$(VERSION) -ldflags '-s'

.PHONY: upload-s3
upload-s3: etcd3-bootstrap-linux-amd64-$(VERSION)
	@if ! git diff HEAD --exit-code &> /dev/null; \
	then \
		echo -e "Unexpected dirty working directory; commit your changes"; \
		exit 1; \
	fi
	aws s3 cp ./etcd3-bootstrap-linux-amd64-$(VERSION) "s3://$(S3_BUCKET)/etcd3-bootstrap-linux-amd64/etcd3-bootstrap-linux-amd64-$(VERSION)"
