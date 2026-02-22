# Build stage
FROM golang:1.22-alpine AS builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /apidoc-gen .

# Runtime stage
FROM alpine:3.19
RUN apk --no-cache add ca-certificates
COPY --from=builder /apidoc-gen /usr/local/bin/apidoc-gen
WORKDIR /workspace
ENTRYPOINT ["/usr/local/bin/apidoc-gen"]
