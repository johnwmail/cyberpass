#!/bin/bash

#buildTime=$(date --rfc-3339=seconds)
buildTime=$(date)
commitHash=$(git rev-parse HEAD)
version="${1:-manual build}"

# static binary
export CGO_ENABLED=0

# go mod init cyberpass
# go mod tidy
go build -ldflags ' -X "main.buildTime='"$buildTime"'" -X "main.commitHash='"${commitHash^^}"'" -X "main.version='"${version^^}"'" '
