# Build stage
FROM golang:1.26-alpine AS builder
WORKDIR /app

# Pass the release version in explicitly: .git is excluded from the build
# context (see .dockerignore), so `git describe` isn't available in here.
ARG VERSION=dev

COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X github.com/devenock/specyl/cmd.version=${VERSION}" -o /specyl .

# Runtime stage
FROM alpine:3.19
RUN apk --no-cache add ca-certificates \
    && addgroup -S specyl && adduser -S specyl -G specyl
COPY --from=builder /specyl /usr/local/bin/specyl
WORKDIR /workspace
USER specyl
ENTRYPOINT ["/usr/local/bin/specyl"]
