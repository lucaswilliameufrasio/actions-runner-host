GO ?= go

.PHONY: prepare fmt vet test check validate-debian validate-image validate-images

## Prepare a VPS for a GitHub Actions self-hosted runner. Does not register the runner.
prepare:
	$(GO) run ./cmd/prepare

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

test:
	$(GO) test -count=1 ./...

check: vet test
	@test -z "$$(gofmt -l .)"

## Run the prepare flow against one image. BASE_IMAGE defaults to debian:13.
validate-image:
	BASE_IMAGE=$${BASE_IMAGE:-debian:13} ./validation/image/validate-one.sh

## Run the prepare flow against Debian 13.
validate-debian:
	BASE_IMAGE=debian:13 ./validation/image/validate-one.sh

## Run the prepare flow against every image in validation/images.txt.
validate-images:
	./validation/validate-images.sh
