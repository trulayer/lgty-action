# Build a static, dependency-free-ish binary (one direct dep: pgx).
#
# Both FROM lines are pinned by digest, not a floating tag — a mutable tag is
# a mutable dependency in the trust path for a public, auditability-first
# repo. Bump digests deliberately in a reviewed PR (Renovate/Dependabot can
# propose the bump; a human still reviews it under the normal CI gate).
FROM golang:1.26@sha256:3aff6657219a4d9c14e27fb1d8976c49c29fddb70ba835014f477e1c70636647 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/lgty-action .

# Minimal, non-root runtime. No shell, no package manager — nothing to exfiltrate with.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:f5b485ea962d9bd1186b2f6b3a061191539b905b82ec395de78cbfae51f20e35
COPY --from=build /out/lgty-action /usr/local/bin/lgty-action
ENTRYPOINT ["/usr/local/bin/lgty-action"]
