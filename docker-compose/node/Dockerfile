ARG DEBIAN_CODENAME=bullseye
FROM debian:${DEBIAN_CODENAME}
ARG DEBIAN_CODENAME=bullseye

RUN apt-get update -q && \
      DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      ca-certificates \
      curl \
      git \
      # tmux term def
      ncurses-term \
      openssh-server \
      # facter complains about missing locales
      locales-all \
      systemd \
      # debuging tools
      jq \
      tree \
      less \
      vim-tiny \
      2>&1
RUN curl -sO https://apt.puppet.com/puppet6-release-${DEBIAN_CODENAME}.deb
RUN dpkg -i puppet6-release-${DEBIAN_CODENAME}.deb
RUN ln -s /dev/null /etc/systemd/system/pxp-agent.service
RUN ln -s /dev/null /etc/systemd/system/puppet.service
RUN apt-get update -q && \
      DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      puppet-agent \
      2>&1
RUN adduser --disabled-password --gecos "heckler" "heckler"
RUN echo 'root:heckler' | chpasswd
COPY setup_ssh_exec_systemd /setup_ssh_exec_systemd
COPY heckler.sh /etc/profile.d/
RUN chmod 755 /setup_ssh_exec_systemd
ADD ssh_configs /ssh_configs
COPY sshd_config /ssh_configs/
RUN sed -i '6i### Inserted by Dockerfile ###\n[ -z "$PS1" ] && export PATH="$PATH:/heckler:/opt/puppetlabs/bin"\n' /etc/bash.bashrc

CMD [ "/setup_ssh_exec_systemd" ]
