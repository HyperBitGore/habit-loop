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

function storeTodo (name, date, complete) {
    todoArray.push({ name, date, complete});
}

function renderTodos (todos) {
    const todoList = document.querySelector("#todo-list");
    const todoSelect = document.querySelector("#todo-select");
    todoList.replaceChildren();
    todoSelect.replaceChildren(new Option("Select a TODO", ""));

    for (const todo of todos) {
        const listItem = document.createElement("li");
        const option = new Option(todo.name, todo.name);
        listItem.textContent = todo.complete ? `${todo.name} (complete)` : todo.name;
        listItem.addEventListener("click", () => openTodoDetail(todo));
        todoList.appendChild(listItem);
        todoSelect.appendChild(option);
    }
}

let detailTodo = null;

function openTodoDetail (todo) {
    detailTodo = todo;
    document.querySelector("#todo-detail-name").textContent = todo.name;
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

export async function deleteSelectedTodo () {
    if (!detailTodo) {
        return;
    }
    const todo = detailTodo;
    try {
        const result = await fetch("http://localhost:8081/api/remove_task", {
            method: "PUT",
            headers: {
                "X-Task-Name": todo.name,
                "X-Task-Date": formatServerDate(todo.date),
                "X-Task-Complete": String(todo.complete)
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
        storeTodo(todo.name, todo.time, todo.complete);
    } catch (error) {
        console.error('Error updating resource:', error);
    }
}
