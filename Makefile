NAME = heckler
SHELL = /bin/bash
BUILD_GIT_SHA = $(shell git rev-parse --short HEAD)
REGISTRY ?= dockerhub.com
REGISTRY_USER ?= heckler
IMAGE = $(REGISTRY)/$(REGISTRY_USER)/$(NAME)-builder
IMAGE_TAGGED = $(IMAGE):$(BUILD_GIT_SHA)
DEBIAN_RELEASE := $(shell . /etc/os-release && echo "$${VERSION_ID}")
DEBIAN_CODENAME ?= $(shell lsb_release -cs)
HECKLER_VERSION := $(shell git describe --abbrev=0 | sed 's/^v//')
BT_VERSION := 2
DEB_VERSION ?= $(HECKLER_VERSION)-$(BT_VERSION)~bt$(DEBIAN_RELEASE)
export CC := /usr/local/musl/bin/musl-gcc 
GO_LDFLAGS := -X main.Version=$(HECKLER_VERSION) -extldflags=-static -linkmode=external
export GOFLAGS := -mod=vendor -tags=static,osusergo
export GOCACHE := $(CURDIR)/.go-build

user_id_real := $(shell id -u)
max_uid_count := 65536
max_minus_uid := $(shell echo $$(( $(max_uid_count) - $(user_id_real) )))
uid_plus_one := $(shell echo $$(( $(user_id_real) + 1)))


.PHONY: help
help: ## Show the help
	@awk \
		'BEGIN { \
			printf "Usage: make <TARGETS>\n\n"; \
			printf "TARGETS:\n"; \
			FS = ":.*?## " \
		}; \
		/^[ a-zA-Z_-]+:.*?## .*$$/ {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}' \
  $(MAKEFILE_LIST)

.PHONY: docker-build
docker-build: docker-build-image.$(DEBIAN_CODENAME) ## Build heckler via the container
	docker run --rm -v $(PWD):/home/builder/$(NAME) $(IMAGE) bash -c "cd /home/builder/$(NAME); make build"

.PHONY: podman-build
podman-build: podman-build-image ## Build heckler via the container
	podman run --uidmap $(user_id_real):0:1  --uidmap 0:1:$(user_id_real) --uidmap $(uid_plus_one):$(uid_plus_one):$(max_minus_uid) --rm -v $(PWD):/home/builder/$(NAME) $(IMAGE) make -C /home/builder/$(NAME) build

.PHONY: docker-vet
docker-vet: docker-build-image.$(DEBIAN_CODENAME) ## Vet heckler via the container
	docker run --rm -v $(PWD):/home/builder/$(NAME) $(IMAGE) make -C /home/builder/$(NAME) vet
.PHONY: podman-vet
podman-vet: podman-build-image ## Vet heckler via the container
	podman run --uidmap $(user_id_real):0:1  --uidmap 0:1:$(user_id_real) --uidmap $(uid_plus_one):$(uid_plus_one):$(max_minus_uid) --rm -v $(PWD):/home/builder/$(NAME) $(IMAGE) make -C /home/builder/$(NAME) vet

.PHONY: docker-test
docker-test: docker-build-image.$(DEBIAN_CODENAME) ## Test heckler via the container
	docker run --rm -v $(PWD):/home/builder/$(NAME) $(IMAGE) make -C /home/builder/$(NAME) test
.PHONY: podman-test
podman-test: podman-build-image ## Test heckler via the container
	podman run --uidmap $(user_id_real):0:1  --uidmap 0:1:$(user_id_real) --uidmap $(uid_plus_one):$(uid_plus_one):$(max_minus_uid) --rm -v $(PWD):/home/builder/$(NAME) $(IMAGE) make -C /home/builder/$(NAME) test

.PHONY: docker-build-image
docker-build-image.$(DEBIAN_CODENAME): Dockerfile.$(DEBIAN_CODENAME) ## Build a docker image used for packaging
	docker build --rm -t $(IMAGE) -t $(IMAGE_TAGGED) - < Dockerfile.$(DEBIAN_CODENAME)

.PHONY: podman-build-image
podman-build-image: Dockerfile.$(DEBIAN_CODENAME) ## Build a docker image used for packaging
	docker build --rm -t $(IMAGE) -t $(IMAGE_TAGGED) - < Dockerfile.$(DEBIAN_CODENAME)

.PHONY: docker-build-deb
docker-build-deb: docker-build-image.$(DEBIAN_CODENAME) ## Build the deb in a local docker container
	docker run --rm -v $(PWD):/home/builder/$(NAME) $(IMAGE) make -C /home/builder/$(NAME) deb DEB_VERSION="$(DEB_VERSION)"

.PHONY: podman-build-deb
podman-build-deb: podman-build-image ## Build the deb in a local docker container
	podman run --uidmap $(user_id_real):0:1  --uidmap 0:1:$(user_id_real) --uidmap $(uid_plus_one):$(uid_plus_one):$(max_minus_uid) --rm -v $(PWD):/home/builder/$(NAME) $(IMAGE) make -C /home/builder/$(NAME) deb DEB_VERSION="$(DEB_VERSION)"

.PHONY: docker-bash
docker-bash: docker-build-image.$(DEBIAN_CODENAME) ## Exec bash in a local docker container
	docker run --rm -it -v $(PWD):/home/builder/$(NAME) $(IMAGE) bash
.PHONY: podman-bash
podman-bash: podman-build-image ## Exec bash in a local docker container
	podman run --uidmap $(user_id_real):0:1  --uidmap 0:1:$(user_id_real) --uidmap $(uid_plus_one):$(uid_plus_one):$(max_minus_uid) --rm -it -v $(PWD):/home/builder/$(NAME) $(IMAGE) bash

.PHONY: publish
publish: ## Upload the deb to package cloud (normally called by Jenkins)
	package_cloud push "braintree/dev-tools/debian/buster" *_$(DEB_VERSION)_*.deb
	package_cloud push "braintree/dev-tools/debian/stretch" *_$(DEB_VERSION)_*.deb

.PHONY: clean
clean: ## Remove all state
	rm -rf vendor/github.com/libgit2/git2go/v31/static-build
	rm -rf vendor/github.com/libgit2/git2go/v31/script
	rm -rf vendor/github.com/libgit2/git2go/v31/vendor
	go clean -cache


.PHONY: build
build: vendor/github.com/libgit2/git2go/v31/static-build ## Build heckler, usually called inside the container
	go build -o . -ldflags '$(GO_LDFLAGS)' ./...

.PHONY: vet
vet: vendor/github.com/libgit2/git2go/v31/static-build ## Vet heckler, usually called inside the container
	go vet ./...

.PHONY: test
test: vendor/github.com/libgit2/git2go/v31/static-build ## Test heckler, usually called inside the container
	go test ./...

vendor/github.com/libgit2/git2go/v31/static-build: ## Build libgit2
	go mod vendor
	./build-libgit2-static

.PHONY: deb
deb: ## Build the deb, usually called inside the container
	rm -f debian/changelog
	dch --create --distribution stable --package $(NAME) -v $(DEB_VERSION) "new version"
	dpkg-buildpackage -us -uc -b
	cp ../*.deb ./

