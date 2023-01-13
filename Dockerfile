FROM golang:1 AS builder

WORKDIR /go/src/saas
COPY . .
RUN make && mv ./saas /

FROM ubuntu:jammy
COPY --from=builder /saas /sbin
# debian python packaging is just the worst and split over a gazillion of packages
# without any python dependencies coccinelle might fail when it requires python
# spatch is notorious for not finding the correct python version, so we help it with 'python-is-python3'
# then it also needs 'libpython3.10', but there is no 'libpython3' meta-package and I don't wwant to
# depend on the exact version (even jammy has python3.11). The minimal version independent package I saw is
# 'libpython3-dev'.
# => python-is-python3 libpython3-dev
RUN apt-get update && \
	apt-get install -y coccinelle ca-certificates make gcc tar libpython3-dev python-is-python3 && \
	apt-get -y clean
RUN mkdir -p /var/cache/saas/patches /var/cache/saas/tarballs

EXPOSE 2020
CMD ["-addr", ":2020"]
ENTRYPOINT ["/sbin/saas"]
