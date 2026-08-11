# syntax=docker/dockerfile:1
FROM golang:1.26 AS builder

WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /bin/gateway ./cmd/gateway
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /bin/upstream ./cmd/upstream

# --- gateway image ---
FROM scratch AS gateway
COPY --from=builder /bin/gateway /gateway
EXPOSE 8080
ENTRYPOINT ["/gateway"]

# --- upstream image ---
FROM scratch AS upstream
COPY --from=builder /bin/upstream /upstream
EXPOSE 9001
ENTRYPOINT ["/upstream"]