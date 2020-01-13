FROM debian:buster

RUN apt-get update && \
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
      tree \
      vim-tiny \
      2>&1
RUN curl -sO https://apt.puppet.com/puppet6-release-buster.deb
RUN dpkg -i puppet6-release-buster.deb
RUN ln -s /dev/null /etc/systemd/system/pxp-agent.service
RUN ln -s /dev/null /etc/systemd/system/puppet.service
RUN apt-get update && \
      DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      puppet-agent \
      2>&1
RUN echo 'root:heckler' | chpasswd
COPY setup_ssh_exec_systemd /setup_ssh_exec_systemd
COPY heckler.sh /etc/profile.d/
RUN chmod 755 /setup_ssh_exec_systemd
ADD ssh_configs /ssh_configs

CMD [ "/setup_ssh_exec_systemd" ]
