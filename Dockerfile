FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /out/api ./cmd/api && \
    CGO_ENABLED=0 go build -o /out/worker ./cmd/worker

FROM alpine:3.22

RUN adduser -D app
USER app

COPY --from=build /out/api /out/worker /usr/local/bin/

CMD ["api"]
