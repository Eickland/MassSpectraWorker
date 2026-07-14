.PHONY: proto proto-python proto-go clean

PROJECT_ROOT := D:/lab/Mass_spectra_worker
PROTO_DIR := src/protobuf

# Генерация всего (Go + Python)
proto: proto-go proto-python
	@echo "All protobuf files generated!"

# Go-генерация (ваша существующая команда)
proto-go:
	@echo "Generating Go protobuf..."
	cd $(PROJECT_ROOT) && \
	protoc -I. \
		--go_out=. \
		--go_opt=paths=source_relative \
		--go-grpc_out=. \
		--go-grpc_opt=paths=source_relative \
		$(PROTO_DIR)/plot.proto
	@echo "Go files generated!"

# Python-генерация (ваша новая команда)
proto-python:
	@echo "Generating Python protobuf..."
	cd $(PROJECT_ROOT) && \
	py.exe -m grpc_tools.protoc -I. \
		--python_out=. \
		--pyi_out=. \
		--grpc_python_out=. \
		$(PROTO_DIR)/plot.proto
	@echo "Python files generated!"

# Очистка всех сгенерированных файлов
clean:
	@rm -f $(PROTO_DIR)/*.pb.go
	@rm -f $(PROTO_DIR)/*_pb2.py
	@rm -f $(PROTO_DIR)/*_pb2.pyi
	@rm -f $(PROTO_DIR)/*_pb2_grpc.py
	@echo "Cleaned all generated files!"