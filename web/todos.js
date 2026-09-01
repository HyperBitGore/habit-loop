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

export function addTodo (name, date, complete) {
    todoArray.push({ name, date, complete});
}

export async function fetchTodos () {
    console.log("Fetching todos");
    try {
        const response = await fetch("http://localhost:8081/api/get_tasks");
        if (!response.ok) {
            throw new Error(`HTTP error! Status: ${response.status}`);
        }
        const data = await response.json(); // Parses JSON response into a JS object
        console.log(data);
    } catch (error) {
        console.error("Failed to fetch todos, ", error.message);
    }
}