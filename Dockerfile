# Build natively, cross-compile via TARGETOS/TARGETARCH — no QEMU needed.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags='-s -w' -o /rf-clipd ./cmd/rf-clipd

FROM scratch
COPY --from=build /rf-clipd /rf-clipd
EXPOSE 8080
VOLUME /data
ENTRYPOINT ["/rf-clipd"]
