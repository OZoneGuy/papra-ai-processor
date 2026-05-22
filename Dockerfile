FROM golang:1.26 AS build

WORKDIR /app
COPY . .

RUN go mod download
RUN CGO_ENABLED=0 go build -o /app/app


FROM gcr.io/distroless/static-debian13

LABEL org.opencontainers.image.title="Papra AI Processor"
LABEL org.opencontainers.image.description="A Go microservice that processes documents from Papra using AI"
LABEL org.opencontainers.image.source="https://github.com/OZoneGuy/papra-ai-processor"
LABEL org.opencontainers.image.documentation="https://github.com/OZoneGuy/papra-ai-processor/blob/main/README.md"
LABEL org.opencontainers.image.license="GNU GPLv3"

COPY --from=build /app/app /

CMD ["/app"]
