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
export GOCACHE := $(CURDIR)/.go-build

.PHONY: help
help: ## Show the help
	@printf 'Usage: make <TARGETS>\n\n'
	@printf 'TARGETS:\n'
	@grep -E '^[ a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
	awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

.PHONY: docker-build
docker-build: docker-build-image ## Build heckler via the container
	docker run --rm -v $(PWD):/home/builder/$(NAME) $(IMAGE) make -C /home/builder/$(NAME) build

.PHONY: docker-build-image
docker-build-image: ## Build a docker image used for packaging
	docker build --rm -t $(IMAGE) -t $(IMAGE_TAGGED) - < Dockerfile

.PHONY: docker-build-deb
docker-build-deb: docker-build-image ## Build the deb in a local docker container
	docker run --rm -v $(PWD):/home/builder/$(NAME) $(IMAGE) make -C /home/builder/$(NAME) deb

.PHONY: docker-bash
docker-bash: docker-build-image ## Exec bash in a local docker container
	docker run --rm -it -v $(PWD):/home/builder/$(NAME) $(IMAGE) bash

.PHONY: publish
publish: ## Upload the deb to package cloud (normally called by Jenkins)
	package_cloud push "braintree/dev-tools/debian/buster" *_$(DEB_VERSION)_*.deb
	package_cloud push "braintree/dev-tools/debian/stretch" *_$(DEB_VERSION)_*.deb

.PHONY: clean
clean: ## Remove all state
	rm -rf vendor/github.com/libgit2/git2go/static-build
	rm -rf vendor/github.com/libgit2/git2go/script
	rm -rf vendor/github.com/libgit2/git2go/vendor
	./debian/rules clean
	go clean -cache


.PHONY: build
build: vendor/github.com/libgit2/git2go/static-build ## Build heckler, usually called inside the container
	go build -o . -mod=vendor -tags=static -ldflags '$(GO_LDFLAGS)' ./...

vendor/github.com/libgit2/git2go/static-build: ## Build libgit2
	./build-libgit2-static

.PHONY: deb
deb: ## Build the deb, usually called inside the container
	rm -f debian/changelog
	dch --create --distribution stable --package $(NAME) -v $(DEB_VERSION) "new version"
	dpkg-buildpackage -us -uc -b
	cp ../*.deb ./

