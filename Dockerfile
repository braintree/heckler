ARG DEBIAN_CODENAME=buster
FROM debian:${DEBIAN_CODENAME}
ARG DEBIAN_CODENAME=buster
ARG USER=builder

ENV DEBIAN_FRONTEND noninteractive

RUN echo "deb http://deb.debian.org/debian ${DEBIAN_CODENAME}-backports main" > /etc/apt/sources.list.d/backports.list
RUN apt-get update \
 && apt-get install -y --no-install-recommends -t ${DEBIAN_CODENAME}-backports \
   build-essential \
   ca-certificates \
   cmake \
   curl \
   debhelper \
   devscripts \
   dh-golang \
   fakeroot \
   git \
   golang \
   less \
   libssl-dev \
   pkg-config \
   sudo \
   tree \
   vim-tiny \
   2>&1

# libgit2 depends on libc & OpenSSL, glibc does not support static linking so
# build against musl and LibreSSL. LibreSSL is used because it builds without
# any issue against musl, whereas Openssl does not.

WORKDIR /usr/local
RUN curl -Ls http://musl.libc.org/releases/musl-1.1.24.tar.gz | tar -xz
WORKDIR musl-1.1.24
RUN ./configure
RUN make install

ENV CC=/usr/local/musl/bin/musl-gcc

WORKDIR /usr/local
RUN curl -Ls https://mirror.planetunix.net/pub/OpenBSD/LibreSSL/libressl-3.0.2.tar.gz | tar -xz
WORKDIR libressl-3.0.2
RUN ./configure --with-openssldir=/etc/ssl
RUN make install

RUN adduser --disabled-password --gecos "docker build user" $USER \
 && echo "$USER ALL=(ALL) NOPASSWD:ALL" >> /etc/sudoers # USER needs sudo to test-install the built .deb

WORKDIR /home/$USER/heckler
USER $USER
