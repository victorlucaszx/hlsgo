FROM golang:1.21-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -o hls-converter .

FROM alpine:3.19

RUN apk add --no-cache ffmpeg ca-certificates

WORKDIR /app

COPY --from=builder /app/hls-converter .

RUN mkdir -p /tmp/hls-conversions

EXPOSE 8001

CMD ["./hls-converter"]
