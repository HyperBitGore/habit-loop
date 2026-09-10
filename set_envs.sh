#!/usr/bin/env bash

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  printf 'Usage: source %s <RESEND_API_KEY> <RESEND_FROM_EMAIL>\n' "$0" >&2
  exit 1
fi

if [[ $# -ne 2 ]]; then
  printf 'Usage: source %s <RESEND_API_KEY> <RESEND_FROM_EMAIL>\n' "${BASH_SOURCE[0]}" >&2
  return 1
fi

export RESEND_API_KEY="$1"
export RESEND_FROM_EMAIL="$2"
