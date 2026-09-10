import { todoOpen, todoClose, fetchTodos, addTodo, todoDetailClose, deleteSelectedTodo, saveSelectedTodo } from "./todos.js";
import {
    editUser,
    deleteUser,
    getCurrentUser,
    getUsers,
    logout,
    createUser
} from "./users.js";
import {
    addHabit,
    changeHabitCalendarMonth,
    closeHabitDetail,
    deleteSelectedHabit,
    fetchHabits,
    saveSelectedHabit
} from "./habits.js";

let currentTodoName = "";
let currentDate = formatDateForServer(new Date());
let currentComplete = false;

function formatDateForServer(date) {
    const pad = (value) => String(value).padStart(2, "0");

    return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ` +
        `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}

function localDateString(date = new Date()) {
    const pad = (value) => String(value).padStart(2, "0");
    return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

function dateTimeForSelectedDate(dateValue) {
    const now = new Date();
    const pad = (value) => String(value).padStart(2, "0");
    return `${dateValue} ${pad(now.getHours())}:${pad(now.getMinutes())}:${pad(now.getSeconds())}`;
}

const todoOpenButton = document.querySelector("#add-todo");
const habitOpenButton = document.querySelector("#add-habit");
const habitPopup = document.querySelector("#habit-popup");
const habitCloseButton = document.querySelector("#habit-close");
const habitForm = document.querySelector("#habit-form");
const habitError = document.querySelector("#habit-error");
const habitDetailCloseButton = document.querySelector("#habit-detail-close");
const habitDetailForm = document.querySelector("#habit-detail-form");
const habitDetailDeleteButton = document.querySelector("#habit-detail-delete");
const habitDetailError = document.querySelector("#habit-detail-error");
const habitCalendarPreviousButton = document.querySelector("#habit-calendar-previous");
const habitCalendarNextButton = document.querySelector("#habit-calendar-next");
const taskDateInput = document.querySelector("#task-date");
const createUserButton = document.querySelector("#create-user");
const manageUsersButton = document.querySelector("#manage-users");
const userPopup = document.querySelector("#user-popup");
const userCloseButton = document.querySelector("#user-close");
const userForm = document.querySelector("#user-form");
const userListPopup = document.querySelector("#user-list-popup");
const userListCloseButton = document.querySelector("#user-list-close");
const userList = document.querySelector("#user-list");
const userListError = document.querySelector("#user-list-error");

async function renderUsers () {
    const users = await getUsers();
    userList.replaceChildren();
    for (const user of users) {
        const form = document.createElement("form");
        form.className = "user-list-row";

        const nameInput = document.createElement("input");
        nameInput.type = "text";
        nameInput.value = user.name;
        nameInput.setAttribute("aria-label", `Username for ${user.name}`);
        nameInput.required = true;

        const roleSelect = document.createElement("select");
        roleSelect.setAttribute("aria-label", `Role for ${user.name}`);
        for (const role of ["user", "admin"]) {
            const option = document.createElement("option");
            option.value = role;
            option.textContent = role === "admin" ? "Admin" : "User";
            option.selected = user.role === role;
            roleSelect.appendChild(option);
        }

        const saveButton = document.createElement("button");
        saveButton.type = "submit";
        saveButton.textContent = "Save";

        const deleteButton = document.createElement("button");
        deleteButton.type = "button";
        deleteButton.textContent = "Delete";
        deleteButton.addEventListener("click", async () => {
            if (!window.confirm(`Delete user "${user.name}"?`)) {
                return;
            }
            userListError.hidden = true;
            try {
                await deleteUser(user.id, user.name);
                await renderUsers();
            } catch (error) {
                userListError.textContent = error.message;
                userListError.hidden = false;
            }
        });

        form.addEventListener("submit", async (event) => {
            event.preventDefault();
            userListError.hidden = true;
            try {
                await editUser(user.id, nameInput.value, roleSelect.value);
                await renderUsers();
            } catch (error) {
                userListError.textContent = error.message;
                userListError.hidden = false;
            }
        });

        form.append(nameInput, roleSelect, saveButton, deleteButton);
        userList.appendChild(form);
    }
}

taskDateInput.value = localDateString();
fetchTodos(taskDateInput.value);
fetchHabits(taskDateInput.value).catch((error) => {
    console.error("Failed to fetch habits:", error);
});

taskDateInput.addEventListener("change", () => {
    currentDate = dateTimeForSelectedDate(taskDateInput.value);
    fetchTodos(taskDateInput.value);
    fetchHabits(taskDateInput.value).catch((error) => {
        console.error("Failed to fetch habits:", error);
    });
});

habitOpenButton.addEventListener("click", () => {
    habitForm.reset();
    habitError.hidden = true;
    habitPopup.hidden = false;
});

habitCloseButton.addEventListener("click", () => {
    habitPopup.hidden = true;
});

habitForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    const formData = new FormData(habitForm);
    habitError.hidden = true;
    try {
        await addHabit(formData.get("name"));
        await fetchHabits(taskDateInput.value);
        habitPopup.hidden = true;
        habitForm.reset();
    } catch (error) {
        habitError.textContent = error.message;
        habitError.hidden = false;
    }
});

habitDetailCloseButton.addEventListener("click", closeHabitDetail);
habitCalendarPreviousButton.addEventListener("click", () => {
    changeHabitCalendarMonth(-1);
});
habitCalendarNextButton.addEventListener("click", () => {
    changeHabitCalendarMonth(1);
});

habitDetailForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    const formData = new FormData(habitDetailForm);
    habitDetailError.hidden = true;
    try {
        await saveSelectedHabit(formData.get("name"), taskDateInput.value);
    } catch (error) {
        habitDetailError.textContent = error.message;
        habitDetailError.hidden = false;
    }
});

habitDetailDeleteButton.addEventListener("click", async () => {
    habitDetailError.hidden = true;
    try {
        await deleteSelectedHabit(taskDateInput.value);
    } catch (error) {
        habitDetailError.textContent = error.message;
        habitDetailError.hidden = false;
    }
});

getCurrentUser()
    .then((user) => {
        if (user.role === "admin") {
            createUserButton.hidden = false;
            manageUsersButton.hidden = false;
        }
    })
    .catch((error) => {
        console.error("Failed to load current user:", error);
    });

createUserButton.addEventListener("click", () => {
    userForm.reset();
    userPopup.hidden = false;
});

manageUsersButton.addEventListener("click", async () => {
    userListError.hidden = true;
    try {
        await renderUsers();
        userListPopup.hidden = false;
    } catch (error) {
        userListError.textContent = error.message;
        userListError.hidden = false;
        userListPopup.hidden = false;
    }
});

userListCloseButton.addEventListener("click", () => {
    userListPopup.hidden = true;
});

userCloseButton.addEventListener("click", () => {
    userPopup.hidden = true;
});

userForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    const formData = new FormData(userForm);
    try {
        await createUser(
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