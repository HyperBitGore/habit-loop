import { apiFetch } from "./api.js";

const container = document.querySelector("#habit-summaries");
const habitsResponse = await apiFetch("/api/get_habits");
if (!habitsResponse.ok) throw new Error(`Unable to load habits. Status: ${habitsResponse.status}`);
const habits = await habitsResponse.json();

for (const habit of habits) {
    const response = await apiFetch(`/api/habit_summary?id=${encodeURIComponent(habit.id)}`);
    if (!response.ok) throw new Error(`Unable to load habit summary. Status: ${response.status}`);
    const entries = await response.json();
    const card = document.createElement("section");
    card.className = "goal-card";
    const heading = document.createElement("div");
    heading.className = "goal-card-heading";
    const title = document.createElement("h2");
    title.textContent = habit.name;
    const count = document.createElement("span");
    count.className = "goal-status";
    count.textContent = `${entries.length} recorded ${entries.length === 1 ? "day" : "days"}`;
    heading.append(title, count);
    card.append(heading);
    const list = document.createElement("ul");
    list.className = "goal-item-list";
    for (const entry of entries) {
        const row = document.createElement("li");
        const states = [];
        if (entry.completed) states.push("completed");
        if (entry.skipped) states.push("skipped");
        row.textContent = `${entry.date.slice(0, 10)} — ${states.join(" and ")}`;
        if (entry.note) {
            const note = document.createElement("div");
            note.className = "history-note";
            note.textContent = entry.note;
            row.append(note);
        }
        list.append(row);
    }
    if (entries.length === 0) {
        const empty = document.createElement("p");
        empty.className = "goal-status";
        empty.textContent = "No completed or skipped dates yet.";
        card.append(empty);
    } else {
        card.append(list);
    }
    container.append(card);
}
