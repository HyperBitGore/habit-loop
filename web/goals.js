import { apiFetch } from "./api.js";

const list = document.querySelector("#goals");
const popup = document.querySelector("#goal-popup");
const items = document.querySelector("#goal-items");

function addItemInput (value = "") {
    const row = document.createElement("div");
    row.className = "goal-item-row";
    const input = document.createElement("input");
    input.required = true;
    input.maxLength = 200;
    input.placeholder = "Next step";
    input.value = value;
    const remove = document.createElement("button");
    remove.type = "button";
    remove.className = "secondary-button";
    remove.textContent = "Remove";
    remove.addEventListener("click", () => row.remove());
    row.append(input, remove);
    items.append(row);
}

function openPopup (goal = null) {
    document.querySelector("#goal-form").reset();
    items.replaceChildren();
    document.querySelector("#goal-id").value = goal?.id || "";
    document.querySelector("#goal-popup-title").textContent = goal ? "Edit goal" : "Add goal";
    document.querySelector("#goal-name").value = goal?.name || "";
    for (const item of goal?.items || []) addItemInput(item.name);
    if (!goal) addItemInput();
    popup.hidden = false;
    document.querySelector("#goal-name").focus();
}

async function updateItem (item) {
    await apiFetch("/api/goals", {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ item_id: item.id, completed: !item.completed })
    });
    await load();
}

function renderGoals (goals) {
    list.replaceChildren();
    for (const goal of goals) {
        const section = document.createElement("section");
        section.className = "goal-card";
        const heading = document.createElement("div");
        heading.className = "goal-card-heading";
        const title = document.createElement("h2");
        title.textContent = goal.name;
        const status = document.createElement("span");
        status.className = "goal-status";
        status.textContent = `${goal.percent}% · ${goal.status}`;
        heading.append(title, status);
        section.append(heading);

        const itemList = document.createElement("ul");
        itemList.className = "task-list goal-task-list";
        for (const item of goal.items) {
            const row = document.createElement("li");
            const name = document.createElement("span");
            name.className = "task-name";
            name.textContent = item.name;
            const complete = document.createElement("button");
            complete.type = "button";
            complete.className = "status-button";
            complete.classList.toggle("is-complete", item.completed);
            complete.textContent = item.completed ? "✓" : "×";
            complete.setAttribute("aria-label", `${item.completed ? "Reopen" : "Complete"} ${item.name}`);
            complete.addEventListener("click", () => updateItem(item));
            row.append(name, complete);
            itemList.append(row);
        }
        section.append(itemList);

        const actions = document.createElement("div");
        actions.className = "goal-card-actions";
        const edit = document.createElement("button");
        edit.className = "secondary-button";
        edit.textContent = "Edit";
        edit.addEventListener("click", () => openPopup(goal));
        actions.append(edit);
        if (goal.status !== "complete") {
            const pause = document.createElement("button");
            pause.className = "secondary-button";
            pause.textContent = goal.status === "active" ? "Pause goal" : "Activate goal";
            pause.addEventListener("click", async () => {
                await apiFetch("/api/goals", {
                    method: "PATCH",
                    headers: { "Content-Type": "application/json" },
                    body: JSON.stringify({
                        id: goal.id,
                        status: goal.status === "active" ? "inactive" : "active"
                    })
                });
                await load();
            });
            actions.append(pause);
        }
        const remove = document.createElement("button");
        remove.className = "secondary-button";
        remove.textContent = "Delete";
        remove.addEventListener("click", async () => {
            if (!window.confirm(`Delete goal "${goal.name}"?`)) return;
            await apiFetch(`/api/goals?id=${goal.id}`, { method: "DELETE" });
            await load();
        });
        actions.append(remove);
        section.append(actions);
        list.append(section);
    }
}

async function load () {
    const response = await apiFetch("/api/goals");
    if (!response.ok) throw new Error(`Unable to load goals. Status: ${response.status}`);
    renderGoals(await response.json());
}

document.querySelector("#add-goal").addEventListener("click", () => openPopup());
document.querySelector("#goal-close").addEventListener("click", () => {
    popup.hidden = true;
});
document.querySelector("#add-goal-item").addEventListener("click", () => addItemInput());
document.querySelector("#goal-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    const response = await apiFetch("/api/goals", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
            id: Number(document.querySelector("#goal-id").value) || 0,
            name: document.querySelector("#goal-name").value,
            items: [...items.querySelectorAll("input")].map((input) => input.value.trim())
        })
    });
    if (!response.ok) throw new Error(`Unable to save goal. Status: ${response.status}`);
    popup.hidden = true;
    await load();
});

load().catch((error) => console.error(error));
