FROM alpine:3.3
MAINTAINER organization_name <help@example.com>
LABEL works.weave.role=system
COPY ./plugin-name /usr/bin/plugin-name
RUN mkdir /lib64 && ln -s /lib/libc.musl-x86_64.so.1 /lib64/ld-linux-x86-64.so.2
ENTRYPOINT ["/usr/bin/plugin-name"]
