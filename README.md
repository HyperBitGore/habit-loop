# Habit Loop

Habit Loop is a small Go web application for logging in and managing daily
habits and todos. The Go server serves the frontend from `web/` and exposes
the task and login API.

## Requirements

- Go 1.27 or newer

## Run locally

From the repository root:

```bash
cd server
export BOOTSTRAP_ADMIN_NAME=owner
export BOOTSTRAP_ADMIN_EMAIL=owner@example.com
export BOOTSTRAP_ADMIN_PASSWORD='replace-with-a-long-password'
go run .
```

Open <http://localhost:8081>. On the first run, the server prompts for an
administrator only through the three server-side bootstrap environment
variables above. Bootstrap creation is allowed only when the database contains
no administrator. Remove the bootstrap variables after the first successful
startup. Public registration can never create an administrator.

Local development defaults to `APP_ENV=development`,
`APP_BASE_URL=http://localhost:8081`, insecure cookies, `storage.db`, and
`../web`.

## Production configuration

Production startup validates its configuration and refuses unsafe defaults:

```bash
export APP_ENV=production
export LISTEN_ADDR=:8081
export DATABASE_PATH=/var/lib/habit-loop/storage.db
export WEB_ROOT=/opt/habit-loop/web
export APP_BASE_URL=https://habit-loop.example.com
export RESEND_API_KEY='...'
export RESEND_FROM_EMAIL='Habit Loop <no-reply@example.com>'
export SECURE_COOKIES=true
export TRUST_PROXY_HEADERS=true
export TRUSTED_PROXY_CIDRS=127.0.0.1/32
```

`APP_BASE_URL` must be an absolute HTTPS URL in production. Verification and
password-reset links are always built from this configured value and never
from the request `Host` header.

Set `TRUST_PROXY_HEADERS=true` only when the server is reachable exclusively
through reverse proxies listed in `TRUSTED_PROXY_CIDRS`. Multiple IP addresses
or CIDRs are comma-separated. A trusted proxy must overwrite `X-Forwarded-For`
and `X-Forwarded-Proto`, terminate HTTPS, and send
`X-Forwarded-Proto: https`; otherwise production requests are rejected.

The server exposes:

- `GET /healthz` for process health.
- `GET /readyz` for database readiness.

It handles `SIGINT` and `SIGTERM` with a bounded graceful shutdown.

## Database migrations

Schema creation runs transactionally at startup. A fresh database starts at
schema version 3 with habit scheduling, `user_id` foreign keys, cascading
deletion, normalized unique emails, token expiration indexes, and unique
habit-date constraints.

Historical upgrade migrations have been removed. Existing databases must
already be at schema version 3. Older versioned databases must be upgraded with
a prior build before starting this version. Unversioned databases are not
converted.

## SQLite backup and restore

Stop writes before taking a filesystem copy. The safest procedure is:

```bash
curl --fail http://127.0.0.1:8081/readyz
# Stop the service gracefully, then:
cp /var/lib/habit-loop/storage.db /var/backups/habit-loop/storage-$(date +%F-%H%M%S).db
```

If WAL files are present, stop the service before copying or use SQLite's
online backup tooling. Restore by stopping the service, preserving the current
database separately, copying the selected backup to `DATABASE_PATH`, fixing
ownership and permissions, and starting the service. Only backups created from
the versioned production schema are supported.

To build a binary instead:

```bash
cd server
go build -o bin/server .
./bin/server
```

## Project layout

```text
server/   Go HTTP server and file-backed user data
web/      HTML, CSS, and JavaScript frontend
```

## API

| Method | Endpoint | Purpose |
| --- | --- | --- |
| `GET` | `/api/login` | Authenticate using `X-User-Name` and `X-User-Password` headers |
| `GET` | `/api/get_tasks?date=YYYY-MM-DD` | List tasks for a date |
| `PUT` | `/api/add_task` | Add a task using task headers |
| `PUT` | `/api/update_task` | Update a task using task headers |
| `PUT` | `/api/remove_task` | Remove a task using `X-Task-ID` |

Task data is currently held in memory and is lost when the server stops.
User files and generated binaries are excluded from version control.

Habit responses include `interval`, `days_mode`, `start_date`,
`days_of_week_id`, and a `days_of_week` object containing boolean `sunday`
through `saturday` fields.
`PUT /api/add_habit` and `PUT /api/edit_habit` accept these optional headers:

- `X-Habit-Interval`: a positive integer number of days.
- `X-Habit-Days-Mode`: `true` to use selected weekdays, otherwise `false`.
- `X-Habit-Start-Date`: the `YYYY-MM-DD` anchor for interval schedules.
- `X-Habit-Days-Of-Week`: a JSON object such as
  `{"monday":true,"wednesday":true,"friday":true}`.

At least one weekday is required when days mode is enabled. Omitted schedule
headers use defaults when creating a habit and preserve the stored values when
editing one.
