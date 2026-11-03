# Build stage
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Install dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o gobelisk .

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /root/

# Copy binary
COPY --from=builder /app/gobelisk .

# Expose ports (SIP UDP/TCP, RTP range, management)
EXPOSE 5060/udp 5060/tcp 5061/tcp 8080/tcp 9090/tcp

# Run
ENTRYPOINT ["./gobelisk"]
CMD ["server"]
