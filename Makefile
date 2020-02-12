NAME = heckler
BUILD_GIT_SHA = $(shell git rev-parse --short HEAD)
IMAGE = dockerhub.braintree.tools/bt/$(NAME)
IMAGE_TAGGED = $(IMAGE):$(BUILD_GIT_SHA)
DEBIAN_RELEASE := $(shell . /etc/os-release && echo "$${VERSION_ID}")
HECKLER_VERSION := $(shell git describe --abbrev=0 | sed 's/^v//')
BT_VERSION := 2
DEB_VERSION := $(HECKLER_VERSION)-$(BT_VERSION)~bt$(DEBIAN_RELEASE)
export CC := /usr/local/musl/bin/musl-gcc 
GO_LDFLAGS := -X main.Version=$(HECKLER_VERSION) -extldflags=-static -linkmode=external
export GOFLAGS := -mod=vendor -tags=static

build: vendor/github.com/libgit2/git2go/static-build
	go build -o . -mod=vendor -tags=static -ldflags '$(GO_LDFLAGS)' ./...

vendor/github.com/libgit2/git2go/static-build:
	./build-libgit2-static

.PHONY: deb
deb:
	rm -f debian/changelog
	dch --create --distribution stable --package $(NAME) -v $(DEB_VERSION) "new version"
	dpkg-buildpackage -us -uc -b -nc
	cp ../*.deb ./

.PHONY: clean
clean:
	rm -rf vendor/github.com/libgit2/git2go/static-build
	rm -rf vendor/github.com/libgit2/git2go/script
	rm -rf vendor/github.com/libgit2/git2go/vendor
	./debian/rules clean
	go clean -cache

.PHONY: publish
publish: ## Upload the deb to package cloud (normally called by Jenkins)
	package_cloud push "braintree/dev-tools/debian/buster" *_$(DEB_VERSION)_*.deb
	package_cloud push "braintree/dev-tools/debian/stretch" *_$(DEB_VERSION)_*.deb

.PHONY: docker-build-image
docker-build-image: ## Build a docker image used for packaging
	docker build --rm -t $(IMAGE) -t $(IMAGE_TAGGED) - < Dockerfile

.PHONY: docker-build-deb
docker-build-deb: docker-build-image ## Build the deb in a local docker container
	docker run --rm -v $(PWD):/home/builder/$(NAME) $(IMAGE) make -C /home/builder/$(NAME) deb

.PHONY: docker-bash
docker-bash: docker-build-image ## Build the deb in a local docker container
	docker run --rm -it -v $(PWD):/home/builder/$(NAME) $(IMAGE) bash

.PHONY: docker-build
docker-build: docker-build-image
	docker run --rm -v $(PWD):/home/builder/$(NAME) $(IMAGE) make -C /home/builder/$(NAME)
