# Build Stage
FROM golang:1.24.3-alpine AS build

WORKDIR /app

# Copy go.mod and go.sum first to leverage Docker cache
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the application
# CGO_ENABLED=0 for static linking
# GOOS=linux to build for Linux
# -a to force rebuilding of packages that are already up-to-date
# -installsuffix cgo to prevent conflicts with cgo-enabled builds
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /app/autoscaler .

# Runtime Stage
FROM alpine:latest

# Create a non-root user and group
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

WORKDIR /app

# Copy the compiled binary from the build stage
COPY --from=build /app/autoscaler /app/autoscaler

# Change ownership of the binary and working directory to the non-root user
# Ensure the /app directory exists and is owned by appuser
RUN chown appuser:appgroup /app/autoscaler && \
    mkdir -p /app && \
    chown appuser:appgroup /app

# Switch to the non-root user
USER appuser

# Expose the default proxy port (for documentation, actual port is configured via env/flag)
EXPOSE 8080

# Set the entrypoint to run the application
ENTRYPOINT ["/app/autoscaler"]