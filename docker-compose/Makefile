run:
	sudo docker run -d \
		--mount type=bind,source=/sys/fs/cgroup,target=/sys/fs/cgroup \
		--mount type=tmpfs,destination=/tmp \
		--mount type=tmpfs,destination=/run \
		--mount type=tmpfs,destination=/run/lock \
		--rm \
		--name node1 \
		dfull:latest

build:
	sudo docker build . -t dfull
