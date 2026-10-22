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

Параметры отдельного спектра (страница «Пакетная обработка», кнопка с ползунками у спектра)

- У каждого спектра могут быть свои параметры; без них берутся параметры задачи.
- Каждый пересчёт — новый прогон со своими файлами (name.svg, name__r2.svg, ...);
  любой успешный прогон можно сделать текущим — он попадает в ZIP.
- API:
  GET  /api/batch/{job}/items/{item}                         — параметры и история прогонов
  POST /api/batch/{job}/items/{item}/reprocess  {"params": {...}}  — пересчёт (можно частично: {"params": {"rel_error": 1}})
  POST /api/batch/{job}/items/{item}/runs/{run}/select       — выбрать прогон
  При создании задачи списком файлов: "items": [{"path": "...", "params": {"charge_max": 3}}]
