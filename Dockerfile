FROM golang:1.17 as build
WORKDIR /go/src/app
ARG GIT_USER
ARG GIT_TOKEN
RUN git config --global url."https://$GIT_USER:$GIT_TOKEN@github.com/".insteadOf "https://github.com/"
ENV GOPRIVATE="github.com/10gen"
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /go/bin/app

FROM debian:buster-slim
RUN apt-get update && apt-get install --yes ca-certificates
RUN groupadd -r app && useradd --no-log-init -r -g app app
USER app
COPY --from=build /go/bin/app /
ENV APP_ADDR ":8080"
EXPOSE 8080
ENTRYPOINT ["/app"]
