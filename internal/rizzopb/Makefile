#	install protoc from source or binary distribution for your platform (last used: 3.15.8)
# go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.30.0
# go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.3.0

all:
	protoc --proto_path=. \
		--go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		*.proto
