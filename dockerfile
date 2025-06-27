# Use official Go image
FROM golang:1.22 AS builder

WORKDIR /app

# Pull latest from GitHub (if using remote clone)
# You can also COPY local files instead if repo is local
# RUN git clone https://github.com/youruser/pricing-admin-service.git .

COPY . .

# Build binary
RUN go mod tidy && go build -o pricing-admin-service .

# Final image
FROM debian:bookworm-slim

WORKDIR /app

COPY --from=builder /app/pricing-admin-service .
COPY --from=builder /app/.env .  


# Optional: copy configs
# COPY --from=builder /app/config ./config

# Expose service port
EXPOSE 8083

CMD ["./pricing-admin-service"]
