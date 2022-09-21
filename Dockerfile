ARG DEBIAN_CODENAME=buster
FROM debian:${DEBIAN_CODENAME}
ARG DEBIAN_CODENAME=buster
ARG MUSL_VERSION=1.2.2
ARG LIBRESSL_VERSION=3.3.3
ARG GO_VERSION=1.16.5
ARG USER=builder

ENV DEBIAN_FRONTEND noninteractive

ENV PATH ${PATH}:/usr/local/go/bin
RUN echo "PATH=$PATH" > /etc/profile

RUN apt-get update \
 && apt-get install -y --no-install-recommends \
   build-essential \
   ca-certificates \
   cmake \
   curl \
   debhelper \
   devscripts \
   dh-golang \
   fakeroot \
   git \
   less \
   libssl-dev \
   pkg-config \
   sudo \
   tree \
   vim-tiny \
   libdistro-info-perl \
   2>&1

WORKDIR /usr/local
RUN curl -Ls https://golang.org/dl/go${GO_VERSION}.linux-amd64.tar.gz | tar -xz

# libgit2 depends on libc & OpenSSL, glibc does not support static linking so
# build against musl and LibreSSL. LibreSSL is used because it builds without
# any issue against musl, whereas Openssl does not.

WORKDIR /usr/local
RUN curl -Ls http://musl.libc.org/releases/musl-${MUSL_VERSION}.tar.gz | tar -xz
WORKDIR musl-${MUSL_VERSION}
RUN ./configure
RUN make install

ENV CC=/usr/local/musl/bin/musl-gcc
ENV GO111MODULE=on

WORKDIR /usr/local
RUN curl -Ls https://mirror.planetunix.net/pub/OpenBSD/LibreSSL/libressl-${LIBRESSL_VERSION}.tar.gz | tar -xz
WORKDIR libressl-${LIBRESSL_VERSION}
RUN ./configure --with-openssldir=/etc/ssl
RUN make install

RUN adduser --disabled-password --gecos "docker build user" $USER \
 && echo "$USER ALL=(ALL) NOPASSWD:ALL" >> /etc/sudoers # USER needs sudo to test-install the built .deb

WORKDIR /home/$USER/heckler
USER $USER
