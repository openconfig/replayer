#!/bin/sh

# Copyright 2022 Google Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Go
which protoc-gen-go > /dev/null
if [ $? -ne 0 ]; then
  echo Installing protoc-gen-go
  go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
  if [ $? -ne 0 ]; then
    echo Failed to install protoc-gen-go
    exit 1
  fi
fi

if [ -z $SRCDIR ]; then
	SRCDIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"
fi

cd ${SRCDIR}

echo Fetching proto dependencies
trap "rm -rf proto/build_deps" EXIT
curl --create-dirs -o proto/build_deps/github.com/grpc/grpc-proto/grpc/binlog/v1/binarylog.proto https://raw.githubusercontent.com/grpc/grpc-proto/master/grpc/binlog/v1/binarylog.proto
if [ $? -ne 0 ]; then
  echo Failed to fetch proto dependencies
  exit 1
fi

protoc -I=".:./proto/build_deps" --go_out=. --go_opt=paths=source_relative proto/log/log.proto
if [ $? -ne 0 ]; then
  echo Failed to build log proto
  exit
fi