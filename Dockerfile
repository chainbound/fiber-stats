FROM golang:1.19-bullseye

WORKDIR /src/app

COPY . .

RUN go mod tidy && go mod verify
RUN go build -o /bin/fiber-stats

CMD ["fiber-stats"]