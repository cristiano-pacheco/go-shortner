FROM golang:1.27.3-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -o /go-shortner .

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
WORKDIR /app
COPY --from=build /go-shortner /app/go-shortner
COPY config/base.yaml /app/config/base.yaml
ENV APP_CONFIG_DIR=/app/config APP_ENV=local
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/app/go-shortner"]
CMD ["management-server"]
# The per-run cleanup label must not invalidate build or runtime image layers.
ARG E2E_BUILD_ID
LABEL io.go-shortner.e2e-build="${E2E_BUILD_ID}"
