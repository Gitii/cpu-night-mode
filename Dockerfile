# Build/test tooling only: the installed daemon runs directly on the host.
ARG UBUNTU_VERSION=26.04
FROM golang:1.26 AS go-toolchain

FROM ubuntu:${UBUNTU_VERSION}
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential ca-certificates debhelper devscripts dpkg-dev fakeroot \
    git lintian make shellcheck systemd \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /work && chmod 1777 /work
COPY --from=go-toolchain /usr/local/go /usr/local/go
ENV PATH="/usr/local/go/bin:${PATH}" GOTOOLCHAIN=local
WORKDIR /work/src
