# --- Build Go API ---
FROM golang:1.26-alpine AS go-builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o bell ./cmd/bell/

# --- Build React SPA ---
FROM node:22-alpine AS web-builder

WORKDIR /build
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ .
RUN npm run build

# --- Final image ---
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app

COPY --from=go-builder /build/bell .
COPY --from=web-builder /build/dist ./web/dist
COPY migrations/ ./migrations/

EXPOSE 8080
CMD ["./bell", "serve"]
