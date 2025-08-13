FROM quay.io/varnish-software/varnish-plus

ENV _GOVERSION=1.24.6
ENV _GOARCH=amd64
USER root
RUN set -ex;\
	mkdir /tmp/go /home/varnish; \
	chown -R varnish /tmp/go /home/varnish; \
	curl -L -o /tmp/go/go.tar.gz https://go.dev/dl/go$_GOVERSION.linux-$_GOARCH.tar.gz; \
	tar -C /usr/local -xzf /tmp/go/go.tar.gz; \
	rm -rf /tmp/go

ENV PATH=$PATH:/usr/local/go/bin

USER varnish
