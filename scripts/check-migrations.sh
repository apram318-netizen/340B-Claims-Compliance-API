#!/usr/bin/env bash
set -euo pipefail

schema_dir="sql/schema"

if [[ ! -d "${schema_dir}" ]]; then
  echo "missing migration directory: ${schema_dir}" >&2
  exit 1
fi

files=()
while IFS= read -r line; do
  files+=("${line}")
done < <(ls "${schema_dir}"/*.sql 2>/dev/null | sort)

if [[ ${#files[@]} -eq 0 ]]; then
  echo "no migration files found in ${schema_dir}" >&2
  exit 1
fi

prev=0

for f in "${files[@]}"; do
  base="$(basename "${f}")"
  prefix="${base%%_*}"

  if [[ ! "${prefix}" =~ ^[0-9]{3}$ ]]; then
    echo "invalid migration prefix in ${base}; expected NNN_description.sql" >&2
    exit 1
  fi

  # shellcheck disable=SC2004
  n=$((10#${prefix}))

  if (( n <= prev )); then
    echo "migration order violation or duplicate prefix detected at ${base}" >&2
    exit 1
  fi
  prev=${n}
done

echo "migration numbering/order looks good (${#files[@]} files)"
