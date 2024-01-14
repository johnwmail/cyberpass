#!/bin/bash

buildTime=$(date --rfc-3339=seconds)
commitHash=$(git rev-parse HEAD)

go build -ldflags '-X "main.buildTime='"$buildTime"'" -X "main.commitHash='"${commitHash^^}"'"' -o cyberpass cyberpass.go
#GOOS=linux GOARCH=amd64 go build -ldflags '-X "main.buildTime='"$buildTime"'" -X "main.commitHash='"${commitHash^^}"'"' -o cyberpass cyberpass.go
#GOOS=linux GOARCH=arm64 go build -ldflags '-X "main.buildTime='"$buildTime"'" -X "main.commitHash='"${commitHash^^}"'"' -o cyberpass cyberpass.go
#GOOS=windows GOARCH=arm64 go build -ldflags '-X "main.buildTime='"$buildTime"'" -X "main.commitHash='"${commitHash^^}"'"' -o cyberpass.exe cyberpass.go


