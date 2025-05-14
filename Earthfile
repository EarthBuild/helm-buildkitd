VERSION 0.8

go:
    FROM golang:1.24.3-alpine

deps:
    FROM +go

    WORKDIR /app

    COPY go.mod go.sum ./
    RUN go mod download

source:
    FROM +deps

    COPY *.go .


build:
    FROM +source

    # Build the application
    # CGO_ENABLED=0 for static linking
    # GOOS=linux to build for Linux
    # -a to force rebuilding of packages that are already up-to-date
    # -installsuffix cgo to prevent conflicts with cgo-enabled builds
    RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /app/autoscaler .

    SAVE ARTIFACT /app/autoscaler

image:
    FROM alpine:latest

    # Create a non-root user and group
    RUN addgroup -S appgroup && adduser -S appuser -G appgroup

    WORKDIR /app

    COPY +build/autoscaler /app/autoscaler

    RUN chown appuser:appgroup /app/autoscaler && \
        mkdir -p /app && \
        chown appuser:appgroup /app

    USER appuser

    # Expose the default proxy port (for documentation, actual port is configured via env/flag)
    EXPOSE 8080

    # Set the entrypoint to run the application
    ENTRYPOINT ["/app/autoscaler"]

    # built-in
    ARG --required EARTHLY_GIT_SHORT_HASH
    SAVE IMAGE 932043103545.dkr.ecr.us-west-2.amazonaws.com/buildkitd-proxy:${EARTHLY_GIT_SHORT_HASH}