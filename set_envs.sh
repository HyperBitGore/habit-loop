#!/usr/bin/env bash

usage() {
  printf '%s\n' \
    "Usage: source ${BASH_SOURCE[0]} <RESEND_API_KEY> <RESEND_FROM_EMAIL> <APP_BASE_URL> [APP_ENV] [LISTEN_ADDR] [DATABASE_PATH] [WEB_ROOT] [SECURE_COOKIES] [TRUST_PROXY_HEADERS] [TRUSTED_PROXY_CIDRS] [BOOTSTRAP_ADMIN_NAME] [BOOTSTRAP_ADMIN_EMAIL] [BOOTSTRAP_ADMIN_PASSWORD]" \
    "" \
    "Defaults:" \
    "  APP_ENV=production" \
    "  LISTEN_ADDR=:8081" \
    "  DATABASE_PATH=storage.db" \
    "  WEB_ROOT=../web" \
    "  SECURE_COOKIES=true" \
    "  TRUST_PROXY_HEADERS=false" \
    "  TRUSTED_PROXY_CIDRS=" >&2
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  usage
  exit 1
fi

if [[ $# -lt 3 || $# -gt 13 ]]; then
  usage
  return 1
fi

export RESEND_API_KEY="$1"
export RESEND_FROM_EMAIL="$2"
export APP_BASE_URL="$3"
export APP_ENV="${4:-production}"
export LISTEN_ADDR="${5:-:8081}"
export DATABASE_PATH="${6:-storage.db}"
export WEB_ROOT="${7:-../web}"
export SECURE_COOKIES="${8:-true}"
export TRUST_PROXY_HEADERS="${9:-false}"
export TRUSTED_PROXY_CIDRS="${10:-}"

if [[ "$TRUST_PROXY_HEADERS" == "true" && -z "$TRUSTED_PROXY_CIDRS" ]]; then
  printf 'TRUSTED_PROXY_CIDRS is required when TRUST_PROXY_HEADERS is true.\n' >&2
  return 1
fi

bootstrap_name="${11:-}"
bootstrap_email="${12:-}"
bootstrap_password="${13:-}"
if [[ -n "$bootstrap_name" || -n "$bootstrap_email" || -n "$bootstrap_password" ]]; then
  if [[ -z "$bootstrap_name" || -z "$bootstrap_email" || -z "$bootstrap_password" ]]; then
    printf 'All BOOTSTRAP_ADMIN_* arguments must be provided together.\n' >&2
    return 1
  fi
  export BOOTSTRAP_ADMIN_NAME="$bootstrap_name"
  export BOOTSTRAP_ADMIN_EMAIL="$bootstrap_email"
  export BOOTSTRAP_ADMIN_PASSWORD="$bootstrap_password"
else
  unset BOOTSTRAP_ADMIN_NAME BOOTSTRAP_ADMIN_EMAIL BOOTSTRAP_ADMIN_PASSWORD
fi

unset bootstrap_name bootstrap_email bootstrap_password
