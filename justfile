build:
  go fmt
  go mod tidy
  go build -v -o build/atom