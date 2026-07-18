# Build a static, dependency-free binary.
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod ./
# No external modules yet; when the pgx driver is added, also `COPY go.sum ./`
# and run `go mod download` here to cache dependencies.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/lgty-action .

# Minimal, non-root runtime. No shell, no package manager — nothing to exfiltrate with.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/lgty-action /usr/local/bin/lgty-action
ENTRYPOINT ["/usr/local/bin/lgty-action"]
