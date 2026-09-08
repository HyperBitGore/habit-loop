import { todoOpen, todoClose, fetchTodos, addTodo, todoDetailClose, deleteSelectedTodo, saveSelectedTodo } from "./todos.js";
import { getCurrentUser, logout, registerUser } from "./users.js";

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
const createUserButton = document.querySelector("#create-user");
const userPopup = document.querySelector("#user-popup");
const userCloseButton = document.querySelector("#user-close");
const userForm = document.querySelector("#user-form");

getCurrentUser()
    .then((user) => {
        if (user.role === "admin") {
            createUserButton.hidden = false;
        }
    })
    .catch((error) => {
        console.error("Failed to load current user:", error);
    });

createUserButton.addEventListener("click", () => {
    userForm.reset();
    userPopup.hidden = false;
});

userCloseButton.addEventListener("click", () => {
    userPopup.hidden = true;
});

userForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    const formData = new FormData(userForm);
    try {
        await registerUser(
            formData.get("name"),
            formData.get("password"),
            formData.get("role")
        );
        userPopup.hidden = true;
        userForm.reset();
    } catch (error) {
        console.error("Failed to create user:", error);
    }
});

const logoutButton = document.querySelector("#logout");
logoutButton.addEventListener("click", logout);

todoOpenButton.addEventListener("click", () => {
    currentTodoName = "";
    todoNameInput.value = "";
    todoOpen();
});

const todoCloseButton = document.querySelector("#todo-close");
todoCloseButton.addEventListener("click", todoClose);

const todoNameInput = document.querySelector("#todo-title");
todoNameInput.addEventListener("input", (event) => {
    currentTodoName = event.target.value;
});

const todoDetailCloseButton = document.querySelector("#todo-detail-close");
todoDetailCloseButton.addEventListener("click", todoDetailClose);

const todoDetailDeleteButton = document.querySelector("#todo-detail-delete");
todoDetailDeleteButton.addEventListener("click", async () => {
    await deleteSelectedTodo();
    await fetchTodos(currentDate.slice(0, 10));
});

const todoDetailSaveButton = document.querySelector("#todo-detail-save");
todoDetailSaveButton.addEventListener("click", async () => {
    const todoDetailNameInput = document.querySelector("#todo-detail-name");
    await saveSelectedTodo(todoDetailNameInput.value);
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