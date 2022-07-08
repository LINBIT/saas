FROM golang:1 AS builder

WORKDIR /go/src/saas
COPY . .
RUN make && mv ./saas /

FROM ubuntu:jammy
COPY --from=builder /saas /sbin
RUN apt-get update && \
	apt-get install -y coccinelle ca-certificates make gcc tar && \
	apt-get -y clean
RUN mkdir -p /var/cache/saas/patches /var/cache/saas/tarballs

EXPOSE 2020
CMD ["-addr", ":2020"]
ENTRYPOINT ["/sbin/saas"]
