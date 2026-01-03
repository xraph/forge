# Multi-stage build demonstrating the fix works in Docker
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copy go.mod and go.sum
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=1 go build -o migration-demo .

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates sqlite-libs

WORKDIR /root/

# Copy the binary
COPY --from=builder /app/migration-demo .

# Before fix: This would panic with "stat .: no such file or directory"
# After fix: Application starts successfully
CMD ["./migration-demo"]

