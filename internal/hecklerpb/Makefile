# go get github.com/gogo/protobuf/proto
# go get github.com/gogo/protobuf/protoc-gen-gogofaster
# go get github.com/gogo/protobuf/gogoproto

all:
	protoc --gogofaster_out=plugins=grpc:. ./*.proto
