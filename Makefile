.PHONY: proto
PROJECT_ROOT := D:/lab/Mass_spectra_worker
PROTO_DIR := src/protobuf

proto:
	@echo "Generating protobuf..."
	cd $(PROJECT_ROOT) && \
	protoc -I. \
		--go_out=. \
		--go_opt=paths=source_relative \
		--go-grpc_out=. \
		--go-grpc_opt=paths=source_relative \
		$(PROTO_DIR)/plot.proto
	@echo "Done!"

clean:
	@rm -f $(PROTO_DIR)/*.pb.go
	@echo "Cleaned!"