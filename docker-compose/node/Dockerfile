FROM debian:buster
ARG HOSTNAME

ADD ssh_configs/${HOSTNAME} /etc/ssh
COPY ssh_configs/ssh_known_hosts /etc/ssh/ssh_known_hosts
RUN mkdir /root/.ssh
RUN chmod 700 /root/.ssh
COPY ssh_configs/${HOSTNAME}/root@${HOSTNAME}_id_ecdsa /root/.ssh/id_ecdsa
COPY ssh_configs/${HOSTNAME}/root@${HOSTNAME}_id_ecdsa.pub /root/.ssh/id_ecdsa.pub
COPY ssh_configs/authorized_keys /root/.ssh/authorized_keys
RUN apt-get update && \
      DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      systemd \
      tree \
      openssh-server 2>&1
RUN echo 'root:test' | chpasswd
RUN sed -i 's/.*PermitRootLogin.*/PermitRootLogin yes/' /etc/ssh/sshd_config

CMD [ "/lib/systemd/systemd" ]
