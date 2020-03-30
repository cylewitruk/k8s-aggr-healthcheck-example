CGO_ENABLED=0
GOOS=linux
GOARCH=amd64

go build -a -tags netgo -ldflags="-s -w" ./healthcheck.go
mkdir -p ./bin
mv ./healthcheck ./bin/
