SHELL = /bin/bash
CONTAINERS = heckler statler waldorf fozzie
SSH_CONFIG_MANAGED_HEADER := HECKLER INTEGRATION TESTS MANAGED SECTION
SSH_KEYS := $(shell echo \
	node/ssh_configs/{heckler,fozzie,statler,waldorf}/root/id_ecdsa{,.pub} \
	node/ssh_configs/{heckler,fozzie,statler,waldorf}/host/ssh_host_ecdsa_key{,.pub} \
	)
SSHABLE_TEST := ssh -o ConnectTimeout=1 -o ConnectionAttempts=1 -o ServerAliveInterval=2
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
ssh-config:	## Set up your ssh_config to "just work" with the containers
	@if ! grep -q '$(SSH_CONFIG_MANAGED_HEADER)' ~/.ssh/config ; then \
		echo -e '\n### BEGIN $(SSH_CONFIG_MANAGED_HEADER) ###\nMatch all\nInclude $(THIS_DIR)/node/ssh_configs/ssh_config\n### END $(SSH_CONFIG_MANAGED_HEADER) ###' >> ~/.ssh/config ; \
		echo Updated ~/.ssh/config to be able to ssh to the test containers. ; \
	else \
		echo ~/.ssh/config is already set up. Remove the section starting with '### BEGIN $(SSH_CONFIG_MANAGED_HEADER) ###' if you want to reset it. ; \
	fi

.PHONY: ssh-keys
ssh-keys: $(SSH_KEYS) ## Generate ssh node keys

$(SSH_KEYS):
	node/gen-ssh-keys heckler fozzie statler waldorf

.PHONY: clean
clean: ## Delete generated ssh keys
	rm -fr node/ssh_configs

.PHONY: integration-test
integration-test: 	# Run the integration tests from TESTING.md as hands-off as possible
ifndef TMUX
	$(error These integration tests require you to be in a tmux session)
endif
	# these integration tests will open and close a bunch of tmux panes. don't go anywhere while this is running
	# set up the tmux panes
	@tmux split-window -h -c $(THIS_DIR)
	@tmux split-window -v -p 80 -c $(THIS_DIR)
	@tmux split-window -v -p 75 -c $(THIS_DIR)
	@tmux split-window -v -p 66 -c $(THIS_DIR)
	@tmux split-window -v -c $(THIS_DIR)
	@tmux select-pane -t 0
	# start the docker containers
	@tmux send-keys -t 1 "make run" Enter
	$(MAKE) ssh-config
	@printf 'Waiting for containers to be sshable...'
	@for container in $(CONTAINERS) ; do \
		while ! $(SSHABLE_TEST) $$container -- true >/dev/null 2>&1; do \
			printf . ; \
			sleep 5 ; \
		done ; \
	done
	@printf '\n'
	# tail the logs (so you can debug easier if something goes wrong)
	@tmux send-keys -t 2 "ssh heckler -- journalctl -f -u hecklerd.service" Enter
	@tmux send-keys -t 3 "ssh statler -- journalctl -f -u rizzod.service" Enter
	@tmux send-keys -t 4 "ssh waldorf -- journalctl -f -u rizzod.service" Enter
	@tmux send-keys -t 5 "ssh fozzie -- journalctl -f -u rizzod.service" Enter
	# sleep for a while to ensure everything has initialized
	sleep 10
	# make heckler apply from the v1 tag
	@HECKLER_RESULTS=$$(ssh heckler -- heckler -rev v1 -force) ; \
		echo "$${HECKLER_RESULTS}" ; \
		if [[ "$${HECKLER_RESULTS}" == 'Applied nodes: (3); Error nodes: (0)' ]] ; then \
		echo "Success!" ; \
	else \
		echo "Failed!" ; \
	fi
	# sleep for a while to ensure heckler applies all subsequent tags
	# TODO try checking its journal logs instead
	# TODO figure out why it's complaining about HTTP 422 Validation Failed [{Resource:Issue Field:assignees Code:invalid Message:}]
	sleep 60
	# TODO run serverspecs here
	# stop the docker containers and close the panes
	@tmux send-keys -t 1 C-c
	@printf 'Waiting for containers to shut down...'
	@for container in $(CONTAINERS) ; do \
		while $(SSHABLE_TEST) $$container -- true >/dev/null 2>&1; do \
			printf . ; \
			sleep 5 ; \
		done ; \
	done
	@printf '\n'
	# TODO close all other panes but only if tests succeeded
	# @tmux kill-pane -a -t 0
	# TODO figure out a way to raise an error if tests failed; all attempts so far have not been lazy evals and caused problems
