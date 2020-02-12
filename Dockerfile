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

# Delete shard ssl libs so gcc's linker chooses the static `.a` libs We don't
# want a fully static library, as that will statically link glibc which is
# considered buggy, there does not appear to be a way to tell gcc to prefer
# static linking.
RUN dpkg -L libssl-dev | grep '\.so$' | xargs rm

RUN adduser --disabled-password --gecos "docker build user" $USER \
 && echo "$USER ALL=(ALL) NOPASSWD:ALL" >> /etc/sudoers # USER needs sudo to test-install the built .deb

WORKDIR /home/$USER/heckler
USER $USER
