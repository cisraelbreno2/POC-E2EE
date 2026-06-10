#!/bin/bash
set -e

echo "Gerando protobuf via Docker..."
docker run --rm -v $(pwd):/workspace -w /workspace golang:latest bash -c "
  apt-get update && apt-get install -y protobuf-compiler && \
  go install google.golang.org/protobuf/cmd/protoc-gen-go@latest && \
  go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest && \
  export PATH=\$PATH:\$(go env GOPATH)/bin && \
  mkdir -p pkg/pb/keyspb pkg/pb/messagespb && \
  protoc --go_out=. --go-grpc_out=. proto/keys.proto && \
  protoc --go_out=. --go-grpc_out=. proto/messages.proto && \
  cp -r e2ee/pkg/pb/* pkg/pb/ && \
  rm -rf e2ee
"
echo "Protobuf gerado com sucesso!"
