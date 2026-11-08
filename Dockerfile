FROM golang:1.27 AS build

WORKDIR /src

COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/server ./cmd/server \
 && CGO_ENABLED=0 go build -o /out/loadgen ./cmd/loadgen

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/server /server
COPY --from=build /out/loadgen /loadgen