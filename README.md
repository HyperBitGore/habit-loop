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
go run .
```

Open <http://localhost:8081>. On the first run, the server prompts for an
admin name and password, then stores the account in `server/users/`.

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
