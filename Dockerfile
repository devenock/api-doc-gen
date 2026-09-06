# Build stage
FROM golang:1.24-alpine AS builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /api-doc-gen .

# Runtime stage
FROM alpine:3.19
RUN apk --no-cache add ca-certificates
COPY --from=builder /api-doc-gen /usr/local/bin/api-doc-gen
WORKDIR /workspace
ENTRYPOINT ["/usr/local/bin/api-doc-gen"]
