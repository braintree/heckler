#!/bin/sh

set -ex

if ! [ -x "$(command -v cmake)" ] ; then
  echo "cmake not found, please install!" >&2
  exit
fi

#ROOT="$(cd "$0/../.." && echo "${PWD}")"

ROOT=${ROOT-"$(cd "$(dirname "$0")/.." && echo "${PWD}")"}
echo "ROOT is $ROOT"
BUILD_PATH="${ROOT}/dynamic-build"
VENDORED_PATH="${ROOT}/vendor/libgit2"

mkdir -p "${BUILD_PATH}/build" "${BUILD_PATH}/install/lib"

cd "${BUILD_PATH}/build" &&
cmake -DTHREADSAFE=ON \
      -DBUILD_CLAR=OFF \
      -DBUILD_SHARED_LIBS=ON \
      -DREGEX_BACKEND=builtin \
      -DCMAKE_C_FLAGS=-fPIC \
      -DCMAKE_BUILD_TYPE="RelWithDebInfo" \
      -DCMAKE_OSX_ARCHITECTURES="x86_64" \
      -DCMAKE_INSTALL_RPATH_USE_LINK_PATH="ON" \
      -DCMAKE_INSTALL_PREFIX="${BUILD_PATH}/install" \
      -DCMAKE_INSTALL_RPATH="${BUILD_PATH}/install/lib" \
      -DCMAKE_MACOSX_RPATH=ON \
      "${VENDORED_PATH}" &&

cmake --build . --target install
