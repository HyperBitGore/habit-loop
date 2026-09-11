# Habit Loop

Habit Loop is a self-hosted habit and todo tracker built with Go, SQLite, and
vanilla JavaScript. It provides date-based todo planning, flexible habit
schedules, account management, and a searchable administration interface.

## Features

- Separate habit and todo lists with completion, skip, and edit controls.
- Custom calendar navigation with Today, Tomorrow, and Yesterday headings.
- Todo deferral to the following calendar day.
- Habits scheduled by an interval of days or selected weekdays.
- Habit history calendar showing completed, skipped, scheduled, and untargeted
  dates.
- Email-verified accounts, password reset, profile editing, and secure
  server-side sessions.
- Cloudflare Turnstile protection for login, registration, and password-reset
  requests.
- Administrator user management with search and cursor-based infinite
  scrolling in pages of 100 users.
- SQLite persistence with transactional schema initialization.

## Requirements

- Go 1.27 or newer
- A Cloudflare Turnstile widget and its secret key
- A Resend API key for email delivery in production

## Local setup

From the repository root:

```bash
cd server
cp server.cfg.example server.cfg
```

Edit `server.cfg` and replace `TURNSTILE_SECRET` with the secret belonging to
the site key configured in `web/turnstile.js`. For the first startup, uncomment
and replace all three `BOOTSTRAP_ADMIN_*` settings:

```ini
BOOTSTRAP_ADMIN_NAME=owner
BOOTSTRAP_ADMIN_EMAIL=owner@example.com
BOOTSTRAP_ADMIN_PASSWORD=replace-with-a-long-password
```

Start the application:

```bash
go run .
```

Open <http://localhost:8081>. Bootstrap administrator creation is allowed only
when the database has no administrator. Clear or remove the bootstrap settings
after the first successful startup. Public registration cannot create
administrators.

The server reads `server.cfg` by default. Select another file with:

```bash
go run . -config /path/to/habit-loop.cfg
```

## Configuration

Configuration uses one `KEY=value` setting per line. Blank lines and lines
beginning with `#` or `;` are ignored. Values may be unquoted, double-quoted,
or single-quoted. Unknown keys, duplicate keys, malformed values, and unsafe
production settings prevent startup.

Relative `DATABASE_PATH` and `WEB_ROOT` values are resolved from the directory
containing the configuration file rather than the current working directory.
Real `server.cfg` files are ignored by Git because they contain secrets.

| Setting | Description |
| --- | --- |
| `APP_ENV` | `development`, `production`, or `test` |
| `LISTEN_ADDR` | HTTP listen address, such as `:8081` |
| `DATABASE_PATH` | SQLite database file |
| `WEB_ROOT` | Directory containing the frontend files |
| `APP_BASE_URL` | Public absolute URL used in verification and reset links |
| `RESEND_API_KEY` | Resend API key; required in production |
| `RESEND_FROM_EMAIL` | Sender address; required in production |
| `TURNSTILE_SECRET` | Secret for the widget site key in `web/turnstile.js` |
| `TURNSTILE_HOSTNAMES` | Comma-separated allowed token hostnames |
| `SECURE_COOKIES` | Whether authentication cookies require HTTPS |
| `TRUST_PROXY_HEADERS` | Whether headers from trusted reverse proxies are used |
| `TRUSTED_PROXY_CIDRS` | Comma-separated trusted proxy IP addresses or CIDRs |
| `BOOTSTRAP_ADMIN_NAME` | Initial administrator username |
| `BOOTSTRAP_ADMIN_EMAIL` | Initial administrator email |
| `BOOTSTRAP_ADMIN_PASSWORD` | Initial administrator password |

The bootstrap administrator settings must either all be present or all be
empty.

## Cloudflare Turnstile

The public widget site key is defined in `web/turnstile.js`. Its matching
secret belongs in `TURNSTILE_SECRET`; a site key cannot be used as the secret.
The widget's Cloudflare Hostname Management settings and
`TURNSTILE_HOSTNAMES` must both include the hostname being used. Hostnames do
not include schemes, ports, or paths.

For local development, authorize `localhost` in Cloudflare and use:

```ini
APP_BASE_URL=http://localhost:8081
TURNSTILE_HOSTNAMES=localhost,127.0.0.1
```

Turnstile protects these actions:

| Flow | Turnstile action |
| --- | --- |
| Login | `login` |
| Public registration | `signup` |
| Password-reset request | `password_reset` |

The server verifies each token with Cloudflare Siteverify and rejects missing,
invalid, expired, replayed, wrong-action, and wrong-hostname tokens.

## Production configuration

Example production configuration:

```ini
APP_ENV=production
LISTEN_ADDR=:8081
DATABASE_PATH=/var/lib/habit-loop/storage.db
WEB_ROOT=/opt/habit-loop/web
APP_BASE_URL=https://todosloop.com
TURNSTILE_SECRET=your-widget-secret
TURNSTILE_HOSTNAMES=todosloop.com
RESEND_API_KEY=your-resend-api-key
RESEND_FROM_EMAIL="Habit Loop <no-reply@example.com>"
SECURE_COOKIES=true
TRUST_PROXY_HEADERS=true
TRUSTED_PROXY_CIDRS=127.0.0.1/32
```

Production requires an HTTPS `APP_BASE_URL`, secure cookies, email
credentials, a Turnstile secret, and a non-local Turnstile hostname that
includes the `APP_BASE_URL` hostname.

Enable `TRUST_PROXY_HEADERS` only when the server is reachable exclusively
through the proxies listed in `TRUSTED_PROXY_CIDRS`. Trusted proxies must
overwrite `X-Forwarded-For` and `X-Forwarded-Proto`, terminate HTTPS, and send
`X-Forwarded-Proto: https`.

Build and run a binary with:

```bash
cd server
go build -o bin/server .
./bin/server -config server.cfg
```

The server handles `SIGINT` and `SIGTERM` with a bounded graceful shutdown and
exposes:

- `GET /healthz` for process health.
- `GET /readyz` for database readiness.

## Habit scheduling

A habit uses one of two scheduling modes:

- **Interval mode:** the habit appears every `interval` days beginning on its
  `start_date`.
- **Days-of-week mode:** the habit appears only on selected weekdays from
  Sunday through Saturday.

Interval calculations use date-only UTC arithmetic. The habit detail calendar
shows completed and skipped dates first, scheduled but unfinished dates in
white, and dates not targeted by the schedule in grey.

Habit responses include `interval`, `days_mode`, `start_date`,
`days_of_week_id`, and a `days_of_week` object containing boolean `sunday`
through `saturday` fields.

`PUT /api/add_habit` and `PUT /api/edit_habit` use:

- `X-Habit-Name`
- `X-Habit-Completions`
- `X-Habit-Interval`
- `X-Habit-Days-Mode`
- `X-Habit-Start-Date`
- `X-Habit-Days-Of-Week`, containing JSON such as
  `{"monday":true,"wednesday":true,"friday":true}`

At least one weekday is required when days-of-week mode is enabled. Omitted
schedule headers use defaults when creating a habit and preserve stored values
when editing one.

## API overview

Authentication is cookie-based. Except for the public account endpoints,
health checks, and static files, API routes require an authenticated session.
Administrator routes additionally require the `admin` role.

| Method | Endpoint | Purpose |
| --- | --- | --- |
| `POST` | `/api/login` | Authenticate with JSON credentials and a Turnstile token |
| `POST` | `/api/logout` | Delete the current session |
| `POST` | `/api/create_account` | Register a public user and send verification email |
| `POST` | `/api/verify-email` | Consume an email-verification token |
| `POST` | `/api/request-password-reset` | Send a password-reset email |
| `POST` | `/api/reset-password` | Consume a password-reset token |
| `GET` | `/api/current_user` | Return the authenticated user |
| `PUT` | `/api/profile` | Update username or email |
| `PUT` | `/api/set_password` | Change the authenticated user's password |
| `GET` | `/api/get_tasks?date=YYYY-MM-DD` | List todos for a date |
| `PUT` | `/api/add_task` | Create a todo using task headers |
| `PUT` | `/api/update_task` | Edit, complete, or defer a todo |
| `DELETE` | `/api/remove_task` | Delete a todo |
| `GET` | `/api/get_habits` | List habits with schedules and history |
| `PUT` | `/api/add_habit` | Create a habit |
| `PUT` | `/api/edit_habit` | Edit a habit and its schedule |
| `DELETE` | `/api/delete_habit` | Delete a habit |
| `POST` | `/api/complete_habit` | Mark a habit completed for a date |
| `POST` | `/api/uncomplete_habit` | Remove a completion |
| `POST` | `/api/skip_habit` | Skip a habit for a date |
| `POST` | `/api/unskip_habit` | Remove a skip |
| `POST` | `/api/register_user` | Create a user as an administrator |
| `GET` | `/api/get_users` | Search and paginate users |
| `PUT` | `/api/edit_user` | Change a username or role |
| `DELETE` | `/api/delete_user` | Delete a user |

`GET /api/get_users` returns at most 100 users in:

```json
{
  "users": [],
  "next_cursor": "..."
}
```

Pass `next_cursor` back as the `cursor` query parameter to load the next page.
The optional `search` parameter performs a case-insensitive substring search
over usernames and email addresses.

## Database

All users, sessions, todos, habits, completions, skips, weekday schedules,
verification tokens, and password-reset tokens are stored in SQLite.

Schema creation runs transactionally. A fresh database starts at schema
version 3 with foreign keys, cascading deletion, normalized unique emails,
token expiration indexes, unique habit-date constraints, and habit scheduling.

Historical upgrade migrations have been removed. Existing databases must
already be at schema version 3. Upgrade older versioned databases with a prior
build before starting this version. Unversioned databases are not converted.

### Backup and restore

Stop writes before taking a filesystem copy:

```bash
curl --fail http://127.0.0.1:8081/readyz
# Stop the service gracefully, then:
cp /var/lib/habit-loop/storage.db /var/backups/habit-loop/storage-$(date +%F-%H%M%S).db
```

If WAL files are present, stop the service before copying or use SQLite's
online backup tooling. Restore by stopping the service, preserving the current
database separately, copying the selected backup to `DATABASE_PATH`, fixing
ownership and permissions, and restarting the service.

## Development

Run the backend checks from `server/`:

```bash
go vet ./...
go test ./...
```

## Project layout

```text
server/                    Go HTTP server, SQLite schema, and tests
server/server.cfg.example  Safe configuration template
web/                       HTML, CSS, and JavaScript frontend
```
