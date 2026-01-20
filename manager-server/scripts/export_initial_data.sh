#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: scripts/export_initial_data.sh [output_dir]

Exports the current database records referenced by XIAOZHI_DATABASE_DSN into
CSV seed files that can be consumed by the application.

Environment variables:
  XIAOZHI_DATABASE_DSN     Connection string compatible with the selected driver.
  XIAOZHI_DATABASE_DRIVER  Database driver (mysql or postgres). Defaults to mysql.
USAGE
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

OUTPUT_DIR=${1:-migrations/data}
DRIVER=${XIAOZHI_DATABASE_DRIVER:-mysql}
DSN=${XIAOZHI_DATABASE_DSN:-}

if [[ -z "$DSN" ]]; then
  echo "XIAOZHI_DATABASE_DSN is not set" >&2
  exit 1
fi

if [[ "$DRIVER" != "mysql" && "$DRIVER" != "postgres" && "$DRIVER" != "postgresql" ]]; then
  echo "Unsupported driver: $DRIVER (expected mysql or postgres)" >&2
  exit 1
fi

go run ./cmd/export-seed -out "$OUTPUT_DIR"

echo "Exported seed data to $OUTPUT_DIR"
