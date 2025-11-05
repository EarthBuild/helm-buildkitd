VERSION 0.8

image:
    BUILD --platform=linux/arm64 --platform=linux/amd64 +saved-platform-image

# tilt-image is used internally in tilt because we only need to build an image for a single platform to save
# time and tilt needs to control the full image name
tilt-image:
    FROM +platform-image

    ARG IMG_NAME
    SAVE IMAGE --push $IMG_NAME

saved-platform-image:
    FROM +platform-image

    ARG --required DEST_REGISTRY
    # built-in
    ARG --required EARTHLY_GIT_SHORT_HASH

    SAVE IMAGE --push $DEST_REGISTRY/buildkitd-proxy:${EARTHLY_GIT_SHORT_HASH}

platform-image:
    FROM gcr.io/distroless/static:latest

    WORKDIR /app

    # Build the go binary on the native arch, leveraging go's cross-compilation abilities to compile for the
    # target arch. This improves performance by skipping compile under emulation
    ARG TARGETARCH
    ARG NATIVEARCH
    COPY --platform=linux/$NATIVEARCH (+build/autoscaler --GOARCH=$TARGETARCH) /app/autoscaler

    # Expose the default proxy port (for documentation, actual port is configured via env/flag)
    EXPOSE 8080

    # Set the entrypoint to run the application
    ENTRYPOINT ["/app/autoscaler"]

    

build:
    FROM +source

    ARG --required GOARCH
    # Build the application
    # CGO_ENABLED=0 for static linking
    # GOOS=linux to build for Linux
    # -a to force rebuilding of packages that are already up-to-date
    # -installsuffix cgo to prevent conflicts with cgo-enabled builds
    RUN CGO_ENABLED=0 GOARCH=$GOARCH GOOS=linux go build -a -installsuffix cgo -o /app/autoscaler .

    SAVE ARTIFACT /app/autoscaler

source:
    FROM +deps

    COPY *.go .

deps:
    FROM +go

    WORKDIR /app

    COPY go.mod go.sum ./
    RUN go mod download

go:
    FROM golang:1.25.4-alpine

