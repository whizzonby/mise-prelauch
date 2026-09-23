# Builds the three Go binaries (api, worker, misectl) into one small image.
# Build context is the repository root.
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY services/api/go.mod services/api/go.sum ./
RUN go mod download
COPY services/api/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ ./cmd/...

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/api /out/worker /out/misectl /app/
EXPOSE 8080
USER nonroot:nonroot
CMD ["/app/api"]
