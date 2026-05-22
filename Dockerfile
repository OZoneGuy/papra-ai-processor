FROM golang:1.26 AS build

WORKDIR /app
COPY . .

RUN go mod download
RUN CGO_ENABLED=0 go build -o /app/app


FROM gcr.io/distroless/static-debian13

COPY --from=build /app/app /

CMD ["/app"]
