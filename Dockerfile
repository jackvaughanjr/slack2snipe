FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY . .
RUN go build -o /app/slack2snipe .

FROM alpine:3.21
COPY --from=builder /app/slack2snipe /app/slack2snipe
ENTRYPOINT ["/app/slack2snipe"]
