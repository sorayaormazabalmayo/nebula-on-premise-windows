#!/bin/bash

# Building the binary that is going to be released 
GOOS=linux GOARCH=amd64 go build -o bin/nebula-on-premise-windows cmd/nebula-on-premise-windows/main.go  

