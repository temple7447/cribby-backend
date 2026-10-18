# Untested in CI here (no Docker daemon was available when this was written).
ARG GO_VERSION=1.26
FROM golang:${GO_VERSION}-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Pure-Go SQLite driver => no cgo, so a static binary on a minimal image.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/seed ./cmd/seed

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /out/seed /
ENV PORT=8000
EXPOSE 8000
# Applies pending migrations, then serves (replaces `alembic upgrade head && uvicorn …`).
ENTRYPOINT ["/server"]
