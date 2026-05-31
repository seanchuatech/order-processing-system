# Stage 1: Build
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy modules files first for caching
COPY go.mod ./
COPY go.mod go.sum* ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary with static linking and disabled CGO
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o server ./cmd/server/main.go

# Stage 2: Final minimal image
FROM gcr.io/distroless/static-debian12:latest

WORKDIR /

# Copy the binary from the builder stage
COPY --from=builder /app/server /server

# Expose port
EXPOSE 8093

# Run the binary
ENTRYPOINT ["/server"]
