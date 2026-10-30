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

Конвертация msconvert (страница «Пакетная обработка», раздел «02 Конвертация»)

- Для .d / .raw / .mzXML сервер вызывает msconvert (MSCONVERT_PATH). Параметры задаются
  в форме задачи и отдельно в окне спектра; поле "convert" в параметрах задачи:
  {"convert": {"peak_picking": "cwt", "cwt_snr": 3, "cwt_peak_space": 0.001,
               "threshold_type": "absolute", "threshold": 2e7,
               "mz_min": 150, "mz_max": 1000, "ms_levels": "1-",
               "extra_filters": ["scanTime [0,600]"]}}
  peak_picking: vendor (алгоритм прибора) | cwt (порог по сигнал/шум) | none (профиль);
  threshold_type: absolute | bpi-relative (доля 0–1) | tic-relative (доля 0–1) | count.
- Готовые .mzML передаются в обработку как есть, параметры конвертации к ним не применяются.
- Сконвертированный файл кэшируется в WORK_DIR/<job>/<имя>.<тег>.mzML; тег зависит от фильтров,
  поэтому пересчёт с другими параметрами конвертирует заново, а с теми же — берёт кэш.
- Умолчания формы — из окружения: PEAK_PICKING (vendor), CWT_SNR (1), CWT_PEAK_SPACE (0.001).
- API: GET /api/msconvert — путь и умолчания; POST /api/msconvert/preview {convert} — итоговая команда.
