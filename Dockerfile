FROM docker.io/library/node:alpine AS build-tailwind

WORKDIR /build

COPY . .

RUN npm i

RUN npm run build-prod

FROM docker.io/library/golang:alpine AS build-go

WORKDIR /build

COPY . .

COPY --from=build-tailwind /build/static/stylesheet.css static/

RUN go build -o out/syncify cmd/main/main.go

FROM docker.io/library/alpine:latest

WORKDIR /app

COPY --from=build-go /build/out/syncify .

EXPOSE 8000

CMD ["./syncify"]
