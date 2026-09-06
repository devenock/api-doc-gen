# Build stage
FROM golang:1.26-alpine AS builder
WORKDIR /app

# Pass the release version in explicitly: .git is excluded from the build
# context (see .dockerignore), so `git describe` isn't available in here.
ARG VERSION=dev

COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X github.com/devenock/api-doc-gen/cmd.version=${VERSION}" -o /api-doc-gen .

# Runtime stage
FROM alpine:3.19
RUN apk --no-cache add ca-certificates \
    && addgroup -S apidocgen && adduser -S apidocgen -G apidocgen
COPY --from=builder /api-doc-gen /usr/local/bin/api-doc-gen
WORKDIR /workspace
USER apidocgen
ENTRYPOINT ["/usr/local/bin/api-doc-gen"]
