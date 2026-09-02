import { todoOpen, todoClose, fetchTodos, addTodo, todoDetailClose, deleteSelectedTodo } from "./todos.js";

let currentTodoName = "";
let currentDate = formatDateForServer(new Date());
let currentComplete = false;

function formatDateForServer(date) {
    const pad = (value) => String(value).padStart(2, "0");

    return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ` +
        `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}

fetchTodos(currentDate.slice(0, 10));

const todoOpenButton = document.querySelector("#add-todo");

todoOpenButton.addEventListener("click", todoOpen);

const todoCloseButton = document.querySelector("#todo-close");
todoCloseButton.addEventListener("click", todoClose);

const todoNameInput = document.querySelector("#todo-title");
todoNameInput.addEventListener("input", (event) => {
    currentTodoName = event.target.value;
});

const todoSelect = document.querySelector("#todo-select");
todoSelect.addEventListener("change", (event) => {
    currentTodoName = event.target.value;
    todoNameInput.value = currentTodoName;
});

const todoDetailCloseButton = document.querySelector("#todo-detail-close");
todoDetailCloseButton.addEventListener("click", todoDetailClose);

const todoDetailDeleteButton = document.querySelector("#todo-detail-delete");
todoDetailDeleteButton.addEventListener("click", async () => {
    await deleteSelectedTodo();
    await fetchTodos(currentDate.slice(0, 10));
});

const todoSaveButton = document.querySelector("#todo-save");
todoSaveButton.addEventListener("click", async () => {
    await addTodo(currentTodoName, currentDate, currentComplete);
    await fetchTodos(currentDate.slice(0, 10));
    todoClose();
    currentTodoName = "";
    currentComplete = false;
});