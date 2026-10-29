# Iki hedef: `api` (HTTP sunucusu) ve `seed` (bir kerelik tohum verisi). docker-compose.yml target ile secer.
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/seed ./cmd/seed

FROM alpine:3.20 AS base
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
# Fatura/irsaliye OCR yuklemeleri (bkz. docs/invoice-ocr.md, UPLOADS_DIR varsayilani "./uploads").
RUN mkdir -p /app/uploads && chown -R nobody:nobody /app
USER nobody

FROM base AS seed
COPY --from=build /out/seed /usr/local/bin/seed
CMD ["seed"]

FROM base AS api
COPY --from=build /out/api /usr/local/bin/api
EXPOSE 8080
CMD ["api"]
