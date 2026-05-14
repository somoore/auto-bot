#!/bin/sh
# Resolve AWS credentials via granted credential-process and export them
# for the Docker container, which doesn't have the granted CLI.
set -e

CREDS=$(granted credential-process --profile "test_AccountA/AdministratorAccess")

export AWS_ACCESS_KEY_ID=$(echo "$CREDS" | python3 -c "import sys,json; print(json.load(sys.stdin)['AccessKeyId'])")
export AWS_SECRET_ACCESS_KEY=$(echo "$CREDS" | python3 -c "import sys,json; print(json.load(sys.stdin)['SecretAccessKey'])")
export AWS_SESSION_TOKEN=$(echo "$CREDS" | python3 -c "import sys,json; print(json.load(sys.stdin)['SessionToken'])")

exec docker compose up "$@"
