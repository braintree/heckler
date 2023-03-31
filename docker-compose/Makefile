SHELL = /bin/bash
SSH_CONFIG_MANAGED_HEADER := HECKLER INTEGRATION TESTS MANAGED SECTION
SSH_CONFIG_IS_MANAGED := $(shell grep '$(SSH_CONFIG_MANAGED_HEADER)' ~/.ssh/config)
SSH_KEYS := $(shell echo \
	node/ssh_configs/{heckler,fozzie,statler,waldorf}/root/id_ecdsa{,.pub} \
	node/ssh_configs/{heckler,fozzie,statler,waldorf}/host/ssh_host_ecdsa_key{,.pub} \
	)
THIS_DIR := $(realpath $(dir $(abspath $(lastword $(MAKEFILE_LIST)))))

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

.PHONY: run
run: build-node-image ## Start docker-compose containers
	docker-compose up --renew-anon-volumes

.PHONY: build-node-image
build-node-image: $(SSH_KEYS) ## Build the node image
	cd node; docker build . -t heckler-node:latest

.PHONY: ssh-config
ssh-config:	## Set up your ssh_config to "just work" with the containers (assumes $GOPATH is set to ~/go)
ifeq ($(SSH_CONFIG_IS_MANAGED),)
	echo -e '\n### BEGIN $(SSH_CONFIG_MANAGED_HEADER) ###\nMatch all\nInclude heckler/ssh_config\n### END $(SSH_CONFIG_MANAGED_HEADER) ###' >> ~/.ssh/config
endif
	ln -sf $(THIS_DIR)/node/ssh_configs $(HOME)/.ssh/heckler

.PHONY: ssh-keys
ssh-keys: $(SSH_KEYS) ## Generate ssh node keys

$(SSH_KEYS):
	node/gen-ssh-keys heckler fozzie statler waldorf

.PHONY: clean
clean: ## Delete generated ssh keys
	rm -fr node/ssh_configs
