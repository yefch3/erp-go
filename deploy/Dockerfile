# One multi-stage Dockerfile for every Go service.
# Build context is the REPOSITORY ROOT; select the service with --build-arg.
ARG SERVICE
FROM golang:1.26-alpine AS build
ARG SERVICE
WORKDIR /src
COPY go.work go.work.sum* ./
COPY pkg/ pkg/
COPY gen/ gen/
COPY services/ services/
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
      -o /out/app ./services/${SERVICE}/cmd

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/app /app
USER nonroot:nonroot
ENTRYPOINT ["/app"]
