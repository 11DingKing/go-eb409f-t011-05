# syntax=docker/dockerfile:1

# Build stage: cross-compile via buildx automatic ARGs so amd64 and arm64 are
# both supported from a single invocation of docker buildx build.
FROM --platform=$BUILDPLATFORM golang:1.26 AS builder
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/dispatch ./cmd/server

# Runtime stage: minimal image carrying only the compiled binary.
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S app && adduser -S -G app -u 10001 app
WORKDIR /app
COPY --from=builder /out/dispatch /app/dispatch
USER app
EXPOSE 48302
ENTRYPOINT ["/app/dispatch"]
