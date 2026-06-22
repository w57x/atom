commit := `git rev-parse --short HEAD 2>/dev/null || echo 'Unknown'`
tag := `git describe --tags --abbrev=0 2>/dev/null || echo 'Unknown'`

build:
  go fmt
  go mod tidy
  go build -v -ldflags="-X 'main.GitCommit={{commit}}' -X 'main.Version={{tag}}'" -o build/atom
