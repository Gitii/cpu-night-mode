UBUNTU_VERSION ?= 26.04
IMAGE := cpu-night-mode-build:$(UBUNTU_VERSION)
DOCKER_RUN = docker run --rm --user "$$(id -u):$$(id -g)" \
	-e HOME=/tmp -e GOCACHE=/tmp/go-cache \
	-e GITHUB_REF -e GITHUB_SHA \
	--mount type=bind,src="$(CURDIR)",dst=/work/src \
	--workdir /work/src $(IMAGE)
VERSION := $(shell sed -n '1p' debian/changelog | cut -d' ' -f2 | tr -d '()' | cut -d- -f1)

.PHONY: all image check lint format deb check-installed install uninstall clean build-inner check-inner lint-inner deb-inner

all: image
	$(DOCKER_RUN) make build-inner

image:
	docker build --build-arg UBUNTU_VERSION=$(UBUNTU_VERSION) -t $(IMAGE) .

check: image
	$(DOCKER_RUN) make check-inner

lint: image
	$(DOCKER_RUN) make lint-inner

format: image
	$(DOCKER_RUN) gofmt -w *.go

deb: image
	$(DOCKER_RUN) make deb-inner

build-inner:
	mkdir -p build
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=$(VERSION)" -o build/cpu-night-mode .

check-inner: build-inner
	go test -race -count=1 ./...
	bash scripts/check-lifecycle.sh build/cpu-night-mode

lint-inner:
	test -z "$$(gofmt -l *.go)"
	go vet ./...
	shellcheck -x scripts/*.sh debian/prerm

deb-inner:
	bash scripts/build-deb.sh

check-installed:
	bash scripts/check-installed.sh

install: deb
	sudo apt-get install ./dist/cpu-night-mode_$(VERSION)-1~ubuntu$(UBUNTU_VERSION)_$$(dpkg --print-architecture).deb

uninstall:
	sudo apt-get remove cpu-night-mode

clean:
	rm -rf build dist
