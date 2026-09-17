import { apiFetch } from "./api.js";

const response = await apiFetch("/api/todo_history");
if (!response.ok) throw new Error(`Unable to load todo history. Status: ${response.status}`);
const tasks = await response.json();
const history = document.querySelector("#history");

for (const task of tasks) {
    const row = document.createElement("article");
    row.className = "history-row";
    const title = document.createElement("div");
    title.className = "history-title";
    title.textContent = `${task.complete ? "✓" : "○"} ${task.name}`;
    const date = document.createElement("time");
    date.dateTime = task.date;
    date.textContent = task.date.slice(0, 10);
    row.append(title, date);
    if (task.note) {
        const note = document.createElement("p");
        note.className = "history-note";
        note.textContent = task.note;
        row.append(note);
    }
    history.append(row);
}
