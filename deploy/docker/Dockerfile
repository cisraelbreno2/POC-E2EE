# syntax=docker/dockerfile:1

FROM golang:1.25.0 AS build
WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/api-gateway ./api-gateway/cmd/main.go
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/keys-service ./keys-service/cmd/main.go
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/messages-service ./messages-service/cmd/main.go

FROM golang:1.25.0 AS dev
WORKDIR /workspace

FROM gcr.io/distroless/static-debian12 AS production
COPY --from=build /bin/api-gateway /bin/api-gateway
COPY --from=build /bin/keys-service /bin/keys-service
COPY --from=build /bin/messages-service /bin/messages-service
