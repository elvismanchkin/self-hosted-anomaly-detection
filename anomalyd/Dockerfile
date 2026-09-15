# Runtime image for a prebuilt static binary (no toolchain in the image):
#   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o anomalyd ./cmd/anomalyd
#   docker build -t anomalyd .
FROM gcr.io/distroless/static-debian13:nonroot
COPY anomalyd /anomalyd
USER nonroot
EXPOSE 9400
ENTRYPOINT ["/anomalyd"]
