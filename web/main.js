import { todoOpen, todoClose, fetchTodos } from "./todos.js";

fetchTodos();

const todoOpenButton = document.querySelector("#add-todo");

todoOpenButton.addEventListener("click", todoOpen);

const todoCloseButton = document.querySelector("#todo-close");
todoCloseButton.addEventListener("click", todoClose);