# syntax=docker/dockerfile:1

# Both base ISOs are built by the release-iso job in
# .github/workflows/release.yml (live-build, unchanged -- full and no-wifi
# variants are two separate full builds, not one image repacked into two)
# and staged into the build context as base.iso / base-nowifi.iso before this
# build runs -- see the build-docker job. Nothing in this Dockerfile runs
# live-build or needs privileged/root build steps.

FROM golang:1.23-alpine AS builder
WORKDIR /build

ARG ISO_NAME=tailboot.iso
ARG ISO_NAME_NOWIFI=tailboot-no-wifi.iso
ARG RELEASE_TAG=dev
ARG CONFIG_OFFSET=0
ARG CONFIG_OFFSET_NOWIFI=0

COPY server/go.mod ./
COPY server/*.go ./
COPY server/static ./static

RUN CGO_ENABLED=0 go build \
      -ldflags "-X main.isoName=${ISO_NAME} -X main.isoNameNoWifi=${ISO_NAME_NOWIFI} -X main.release=${RELEASE_TAG} -X main.configOffsetStr=${CONFIG_OFFSET} -X main.configOffsetNoWifiStr=${CONFIG_OFFSET_NOWIFI}" \
      -o /tailboot-server .

FROM alpine:3.20
RUN adduser -D -H tailboot
COPY --from=builder /tailboot-server /usr/local/bin/tailboot-server
COPY base.iso /data/base.iso
COPY base-nowifi.iso /data/base-nowifi.iso
USER tailboot
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/tailboot-server"]
