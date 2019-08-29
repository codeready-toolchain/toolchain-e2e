FROM registry.access.redhat.com/ubi7/ubi-minimal:latest

LABEL maintainer "Devtools <devtools@redhat.com>"
LABEL author "Devtools <devtools@redhat.com>"

ENV OPERATOR=/usr/local/bin/toolchain-e2e \
    USER_UID=1001 \
    USER_NAME=toolchain-e2e \
    LANG=en_US.utf8

# install operator binary
COPY build/_output/bin/toolchain-e2e ${OPERATOR}

COPY build/bin /usr/local/bin
RUN  /usr/local/bin/user_setup

ENTRYPOINT ["/usr/local/bin/entrypoint"]

USER ${USER_UID}
