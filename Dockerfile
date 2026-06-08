# ==========================================
# 🐳 Step 1: Build Stage
# ==========================================
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Compile Go application as statically-linked binary (CGO_ENABLED=0)
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o server main.go

# ==========================================
# 🐳 Step 2: Runtime Stage
# ==========================================
FROM alpine:latest

WORKDIR /app

# Install ca-certificates for external https calls (like DeepSeek API)
RUN apk add --no-cache ca-certificates tzdata

# Copy compiled binary from builder
COPY --from=builder /app/server .

# Create directory for SQLite database storage (to support volume mounting)
RUN mkdir -p /app/data

# Environment variable defaults
ENV PORT=8080
ENV DATABASE_URL=/app/data/maybe.db
ENV ALLOWED_ORIGIN=http://localhost:5173

# Expose API port
EXPOSE 8080

# Execute server binary
CMD ["./server"]
