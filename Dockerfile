# --- UI -----------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM node:22-alpine AS ui
WORKDIR /ui
COPY ui/package.json ui/package-lock.json ./
RUN npm ci
COPY ui .
RUN npm run build

# --- Go -----------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui /ui/dist ./ui/dist
ARG VERSION=dev
ARG TARGETOS TARGETARCH
# cross-compile on the build host instead of emulating the target (much faster under buildx)
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /murmelmoney .

# --- Runtime ------------------------------------------------------------------
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /murmelmoney /murmelmoney
VOLUME /pb_data
EXPOSE 8070
ENTRYPOINT ["/murmelmoney", "serve", "--http=0.0.0.0:8070", "--dir=/pb_data"]
