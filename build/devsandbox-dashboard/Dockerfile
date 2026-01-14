FROM registry.access.redhat.com/ubi9/ubi:latest as build-tools

LABEL maintainer="Developer Sandbox <devsandbox@redhat.com>"
LABEL author="Developer Sandbox <devsandbox@redhat.com>"

ENV GOPATH=/tmp/go \
    GOCACHE=/tmp/.cache \
    PATH=$PATH:$GOPATH/bin \
    GIT_COMMITTER_NAME="Developer Sandbox" \
    GIT_COMMITTER_EMAIL=devsandbox@redhat.com \
    GOLANG_VERSION=go1.24.11 \
    GOLANG_SHA256=bceca00afaac856bc48b4cc33db7cd9eb383c81811379faed3bdbc80edb0af65

# Install additional tools
# gettext for envsubst
RUN yum install -y \
        findutils \
        git \
        make \
        gettext \
        procps-ng \
        tar \
        wget \
        which \
        jq \
        gcc \
        gcc-c++ \
        python3 \
        python3-devel \
        sqlite-devel && \
    yum clean all

# Install oc CLI
RUN curl -LO https://mirror.openshift.com/pub/openshift-v4/clients/ocp/latest/openshift-client-linux.tar.gz && \
    tar -xzvf openshift-client-linux.tar.gz -C /usr/local/bin oc && \
    chmod +x /usr/local/bin/oc && \
    rm -f openshift-client-linux.tar.gz

# Download, verify, and install Go
ENV PATH=$PATH:/usr/local/go/bin
WORKDIR /tmp
RUN curl -Lo ${GOLANG_VERSION}.linux-amd64.tar.gz https://dl.google.com/go/${GOLANG_VERSION}.linux-amd64.tar.gz \
    && echo "${GOLANG_SHA256} ${GOLANG_VERSION}.linux-amd64.tar.gz" > ${GOLANG_VERSION}.linux-amd64.sha256 \
    && sha256sum -c ${GOLANG_VERSION}.linux-amd64.sha256 \
    && tar -C /usr/local -xzf ${GOLANG_VERSION}.linux-amd64.tar.gz \
    && rm -f ${GOLANG_VERSION}.linux-amd64.tar.gz \
    && go version

WORKDIR /app

# Increase shared memory for browsers (recommended for Playwright)
RUN mkdir -p /dev/shm && chmod 1777 /dev/shm

# Install Firefox required dependencies
RUN yum install -y \
        libxcb \
        libX11 \
        libXext \
        libXrandr \
        libXcomposite \
        libXcursor \
        libXdamage \
        libXfixes \
        libXi \
        gtk3 \
        pango \
        atk \
        cairo \
        gdk-pixbuf2 \
        libXrender \
        alsa-lib \
        freetype \
        fontconfig \
        libX11-xcb && \
    yum clean all

COPY . .

CMD ["/bin/bash"]
