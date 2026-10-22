Установка

1. sudo apt install -y protobuf-compiler
2. go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
3. go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
4. export PATH="$PATH:$(go env GOPATH)/bin"
5. make proto

Зависимости Go (после перехода на SQLite, один раз)

6. go get modernc.org/sqlite@latest && go mod tidy   (или: make deps)

Запуск

1. Python gRPC-сервис:  python -m src.server.main
2. Go HTTP-сервер:      go run ./cmd/api   → http://localhost:8080
   База SQLite (DB_PATH, по умолчанию ./data/massspectra.db) создаётся сама.
3. Тесты Go:            go test ./internal/...
