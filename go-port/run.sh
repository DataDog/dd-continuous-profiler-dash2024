#!/bin/bash

# Used in workshop environment, harmless for others
export PATH=$PATH:/usr/lib/go-1.22/bin/

DD_ENV=staging DD_SERVICE=movies-api-go DD_VERSION=`date +"%y%m%d_%H%M%S"` DD_PROFILING_ENABLED=true DD_PROFILING_EXECUTION_TRACE_PERIOD=1s go run main.go
