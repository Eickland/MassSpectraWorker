Установка

1. sudo apt install -y protobuf-compiler
2. go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
3. go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
4. export PATH="$PATH:$(go env GOPATH)/bin"
5. make proto