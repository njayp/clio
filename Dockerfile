FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 go build -o clio .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/clio /usr/local/bin/clio
USER 65534:65534
ENTRYPOINT ["clio"]
