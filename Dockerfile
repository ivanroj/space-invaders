# Builds the Wasm package and the server, then ships both in a small image.
#
#   docker build -t space-invaders .
#   docker run -p 8080:8080 -e JWT_SECRET=$(openssl rand -hex 32) -v invaders:/data space-invaders
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" -o web/main.wasm ./cmd/wasm \
 && cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" web/wasm_exec.js \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server \
 && mkdir /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
COPY --from=build /src/web /web
COPY --from=build --chown=nonroot:nonroot /out/data /data
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/server", "-addr", ":8080", "-web", "/web", "-data", "/data/store.json"]
