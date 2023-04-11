FROM golang:1.20.3

RUN apt-get update \
    && apt-get install -y tor

COPY . /app
WORKDIR /app

RUN go get

ENTRYPOINT [ "go", "run", "/app/main.go" ]