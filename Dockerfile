# syntax=docker/dockerfile:1

# The base ISO is built by the release-iso job in .github/workflows/release.yml
# (live-build, unchanged) and staged into the build context as base.iso before
# this build runs -- see the build-docker job. Nothing in this Dockerfile runs
# live-build or needs privileged/root build steps.

FROM golang:1.23-alpine AS builder
WORKDIR /build

ARG ISO_NAME=tailboot.iso
ARG RELEASE_TAG=dev
ARG CONFIG_OFFSET=0

COPY server/go.mod ./
COPY server/*.go ./
COPY server/static ./static

RUN CGO_ENABLED=0 go build \
      -ldflags "-X main.isoName=${ISO_NAME} -X main.release=${RELEASE_TAG} -X main.configOffsetStr=${CONFIG_OFFSET}" \
      -o /tailboot-server .

FROM alpine:3.20
RUN adduser -D -H tailboot
COPY --from=builder /tailboot-server /usr/local/bin/tailboot-server
COPY base.iso /data/base.iso
USER tailboot
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/tailboot-server"]
