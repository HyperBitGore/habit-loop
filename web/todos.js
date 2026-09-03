let todoArray = [];

export function todoOpen () {
    showTodo(false);
}

export function todoClose () {
    showTodo(true);
}

export function showTodo (hide) {
    const popup = document.querySelector("#todo-popup");
    popup.hidden = hide;
}

function renderTodos (todos) {
    const todoList = document.querySelector("#todo-list");
    todoList.replaceChildren();
    todoArray = todos;
    for (const todo of todos) {
        const listItem = document.createElement("li");
        const todoName = document.createElement("span");
        todoName.textContent = todo.name;
        const completeButton = document.createElement("button");
        completeButton.type = "button";
        completeButton.textContent = todo.complete ? "✓" : "X";
        completeButton.setAttribute(
            "aria-label",
            todo.complete ? `Mark ${todo.name} incomplete` : `Mark ${todo.name} complete`
        );
        completeButton.addEventListener("click", async (event) => {
            event.stopPropagation();
            await toggleTodoComplete(todo);
            await fetchTodos(formatLocalDate(new Date()));
        });
        listItem.addEventListener("click", () => openTodoDetail(todo));
        listItem.append(todoName, completeButton);
        todoList.appendChild(listItem);
    }
}

let detailTodo = null;

function openTodoDetail (todo) {
    detailTodo = todo;
    document.querySelector("#todo-detail-name").value = todo.name;
    document.querySelector("#todo-detail-date").textContent = formatServerDate(todo.date);
    document.querySelector("#todo-detail-complete").textContent = todo.complete ? "Yes" : "No";
    document.querySelector("#todo-detail-popup").hidden = false;
}

export function todoDetailClose () {
    detailTodo = null;
    document.querySelector("#todo-detail-popup").hidden = true;
}

// Converts an RFC 3339 date from the server into the "2006-01-02 15:04:05" layout
function formatServerDate (date) {
    return date.slice(0, 19).replace("T", " ");
}

function formatLocalDate (date) {
    const pad = (value) => String(value).padStart(2, "0");
    return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

export async function saveSelectedTodo (name) {
    if (!detailTodo) {
        return;
    }
    const todo = detailTodo;
    await editTodo(todo.id, name, todo.date, todo.complete);
    todoDetailClose();
}

export async function toggleTodoComplete (todo) {
    await editTodo(todo.id, todo.name, todo.date, !todo.complete);
}

export async function deleteSelectedTodo () {
    if (!detailTodo) {
        return;
    }

    const todo = detailTodo;
    try {
        const result = await fetch("http://localhost:8081/api/remove_task", {
            method: "PUT",
            headers: {
                "X-Task-ID": String(todo.id)
            }
        })
        if (!result.ok) {
            throw new Error(`HTTP error! Status: ${result.status}`);
        }
        todoDetailClose();
    } catch (error) {
        console.error("Error deleting todo:", error);
    }
}

export async function fetchTodos (date) {
    console.log("Fetching todos");
    try {
        const response = await fetch(
            `http://localhost:8081/api/get_tasks?date=${encodeURIComponent(date)}`
        );
        if (!response.ok) {
            throw new Error(`HTTP error! Status: ${response.status}`);
        }
        const data = await response.json();
        renderTodos(data);
    } catch (error) {
        console.error("Failed to fetch todos, ", error.message);
    }
}

function constructTodo (name, time, complete) {
    return Object.seal({ name, time, complete })
}

export async function addTodo (name, time, complete) {
    console.log("Adding todo")
    const todo = constructTodo(name, time, complete)
    try {
        const result = await fetch("http://localhost:8081/api/add_task", {
            method: "PUT",
            headers: {
                "X-Task-Name": name,
                "X-Task-Date": time,
                "X-Task-Complete": String(complete)
            }
        })
        if (!result.ok) {
            throw new Error(`HTTP error! Status: ${result.status}`);
        }
    } catch (error) {
        console.error('Error updating resource:', error);
    }
}

export async function editTodo (todo_id, todo_name, todo_date, todo_complete) {
    const todo = todoArray.find((e) => {
        return e.id == todo_id;
    })
    if (todo == undefined) {
        return;
    }
    console.log(todo);
    try {
        const result = await fetch("http://localhost:8081/api/update_task", {
            method: "PUT",
            headers: {
                "X-Task-Name": todo_name,
                "X-Task-Date": todo_date,
                "X-Task-Complete": String(todo_complete),
                "X-Task-ID": String(todo.id)
            }
        });
        if (!result.ok) {
            throw new Error(`HTTP error! Status: ${result.status}`);
        }
    } catch (error) {
        console.error("Error updating resource: ", error);
    }
}