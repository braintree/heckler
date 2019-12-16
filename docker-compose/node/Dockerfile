FROM debian:buster

RUN apt-get update && \
      DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      ca-certificates \
      curl \
      git \
      openssh-server \
      systemd \
      tree \
      vim-tiny \
      2>&1
RUN echo 'root:heckler' | chpasswd
COPY setup_ssh_exec_systemd /setup_ssh_exec_systemd
RUN chmod 755 /setup_ssh_exec_systemd
ADD ssh_configs /ssh_configs

CMD [ "/setup_ssh_exec_systemd" ]
