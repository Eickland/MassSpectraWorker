// go.mod
module MassSpectraWorker

go 1.26.7

require (
	github.com/go-chi/chi/v5 v5.3.2
	github.com/google/uuid v1.6.0
	github.com/jmoiron/sqlx v1.4.0
	google.golang.org/grpc v1.83.2
	google.golang.org/protobuf v1.36.12
)

require github.com/lib/pq v1.12.3

require (
	github.com/joho/godotenv v1.5.1
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260831171406-18b4a7587f8a // indirect
)
