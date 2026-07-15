# Универсальный Makefile для WSL/Linux/macOS
.PHONY: proto proto-python proto-go clean

PROJECT_ROOT := .
PROTO_DIR := src/protobuf

# Определяем Python команду в зависимости от ОС
ifeq ($(OS),Windows_NT)
    PYTHON_CMD := python
    RM_CMD := del /Q
    SEP := \\
else
    PYTHON_CMD := python3
    RM_CMD := rm -f
    SEP := /
endif

# Генерация всего
proto: proto-go proto-python
	@echo "✅ All protobuf files generated!"

# Go-генерация
proto-go:
	@echo "Generating Go protobuf..."
	@cd $(PROJECT_ROOT) && \
	protoc -I. \
		--go_out=. \
		--go_opt=paths=source_relative \
		--go-grpc_out=. \
		--go-grpc_opt=paths=source_relative \
		$(PROTO_DIR)/plot.proto
	@echo "✅ Go files generated!"

# Python-генерация
proto-python:
	@echo "Generating Python protobuf..."
	@cd $(PROJECT_ROOT) && \
	$(PYTHON_CMD) -m grpc_tools.protoc -I. \
		--python_out=. \
		--pyi_out=. \
		--grpc_python_out=. \
		$(PROTO_DIR)/plot.proto
	@echo "✅ Python files generated!"

# Очистка
clean:
	@echo "Cleaning generated files..."
	@cd $(PROJECT_ROOT) && \
	$(RM_CMD) $(PROTO_DIR)/*.pb.go 2>/dev/null || true
	@cd $(PROJECT_ROOT) && \
	$(RM_CMD) $(PROTO_DIR)/*_pb2.py 2>/dev/null || true
	@cd $(PROJECT_ROOT) && \
	$(RM_CMD) $(PROTO_DIR)/*_pb2.pyi 2>/dev/null || true
	@cd $(PROJECT_ROOT) && \
	$(RM_CMD) $(PROTO_DIR)/*_pb2_grpc.py 2>/dev/null || true
	@echo "✅ Cleaned all generated files!"

# Отладка - показать настройки
debug:
	@echo "OS: $(OS)"
	@echo "PYTHON_CMD: $(PYTHON_CMD)"
	@echo "RM_CMD: $(RM_CMD)"
	@echo "PROJECT_ROOT: $(PROJECT_ROOT)"
	@echo "PROTO_DIR: $(PROTO_DIR)"