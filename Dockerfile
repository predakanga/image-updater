# TODO: Investigate nonroot tag
FROM gcr.io/distroless/static:latest

COPY image-updater /usr/bin/image-updater
ENTRYPOINT ["/usr/bin/image-updater"]