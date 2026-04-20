#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# Requires Docker and openapi-generator-cli image.
docker run --rm -v "${ROOT}:/local" openapitools/openapi-generator-cli:v7.2.0 generate \
  -i /local/api/openapi.yaml \
  -g go \
  -o /local/gen/openapi-go-client \
  --additional-properties=packageName=claimsclient,isGoSubmodule=true

echo "Generated client into gen/openapi-go-client (add to .gitignore if desired)."
