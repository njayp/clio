FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o clio ./cmd/clio

FROM golang:1.26-alpine
RUN apk add --no-cache git nodejs npm ca-certificates && \
    npm install -g @anthropic-ai/claude-code
COPY --from=builder /app/clio /usr/local/bin/clio
ENTRYPOINT ["clio"]
