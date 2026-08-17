# --- UI -----------------------------------------------------------------------
FROM node:22-alpine AS ui
WORKDIR /ui
COPY ui/package.json ui/package-lock.json ./
RUN npm ci
COPY ui .
RUN npm run build

# --- Go -----------------------------------------------------------------------
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui /ui/dist ./ui/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /murmelmoney .

# --- Runtime ------------------------------------------------------------------
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /murmelmoney /murmelmoney
VOLUME /pb_data
EXPOSE 8070
ENTRYPOINT ["/murmelmoney", "serve", "--http=0.0.0.0:8070", "--dir=/pb_data"]
