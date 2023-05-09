NAME = heckler
BUILD_GIT_SHA = $(shell git rev-parse --short HEAD)
IMAGE = dockerhub.com/heckler/$(NAME)
IMAGE_TAGGED = $(IMAGE):$(BUILD_GIT_SHA)
DEBIAN_RELEASE := $(shell . /etc/os-release && echo "$${VERSION_ID}")
HECKLER_VERSION := $(shell git describe --abbrev=0 | sed 's/^v//')
BT_VERSION := 2
DEB_VERSION := $(HECKLER_VERSION)-$(BT_VERSION)~bt$(DEBIAN_RELEASE)
export CC := /usr/local/musl/bin/musl-gcc 
GO_LDFLAGS := -X main.Version=$(HECKLER_VERSION) -extldflags=-static -linkmode=external
export GOFLAGS := -mod=vendor -tags=static,osusergo
export GOCACHE := $(CURDIR)/.go-build

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
docker-build: docker-build-image ## Build heckler via the container
	docker run --rm -v $(PWD):/home/builder/$(NAME) $(IMAGE) make -C /home/builder/$(NAME) build

.PHONY: docker-vet
docker-vet: docker-build-image ## Vet heckler via the container
	docker run --rm -v $(PWD):/home/builder/$(NAME) $(IMAGE) make -C /home/builder/$(NAME) vet

.PHONY: docker-test
docker-test: docker-build-image ## Test heckler via the container
	docker run --rm -v $(PWD):/home/builder/$(NAME) $(IMAGE) make -C /home/builder/$(NAME) test

.PHONY: docker-build-image
docker-build-image: ## Build a docker image used for packaging
	docker build --rm -t $(IMAGE) -t $(IMAGE_TAGGED) - < Dockerfile

.PHONY: docker-build-deb
docker-build-deb: docker-build-image ## Build the deb in a local docker container
	docker run --rm -v $(PWD):/home/builder/$(NAME) $(IMAGE) make -C /home/builder/$(NAME) deb

.PHONY: docker-bash
docker-bash: docker-build-image ## Exec bash in a local docker container
	docker run --rm -it -v $(PWD):/home/builder/$(NAME) $(IMAGE) bash

#	Before running this, you must install `protoc` from source or binary
#	distribution for your platform, as well as the go/grpc plugins.
#	The last used version of these is automatically placed in the headers of the
#	generated files (look for `*.pb.go` files.)
#	See the following links for more info (keeping in mind version differences):
#		* https://grpc.io/docs/protoc-installation
#		* https://grpc.io/docs/languages/go/quickstart
.PHONY: protoc
protoc:	## Regenerate gRPC code (hecklerpb etc. packages) from .proto files
	protoc --proto_path=. \
		--go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		$$(find . -name '*.proto' -type f)

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
