import { todoOpen, todoClose, fetchTodos, addTodo, todoDetailClose, deleteSelectedTodo, saveSelectedTodo } from "./todos.js";
import { getCurrentUser, logout } from "./users.js";
import { initializeDatePicker } from "./date-picker.js";
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

function updateSelectedDayHeading (dateValue) {
    const heading = document.querySelector("#selected-day-heading");
    const selectedDate = new Date(`${dateValue}T00:00:00`);
    const today = new Date();
    const tomorrow = new Date(today.getFullYear(), today.getMonth(), today.getDate() + 1);
    const yesterday = new Date(today.getFullYear(), today.getMonth(), today.getDate() - 1);

    if (dateValue === localDateString(today)) {
        heading.textContent = "Today";
    } else if (dateValue === localDateString(tomorrow)) {
        heading.textContent = "Tomorrow";
    } else if (dateValue === localDateString(yesterday)) {
        heading.textContent = "Yesterday";
    } else {
        heading.textContent = new Intl.DateTimeFormat(undefined, {
            month: "long",
            day: "numeric"
        }).format(selectedDate);
    }
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
const settingsToggle = document.querySelector("#settings-toggle");
const settingsDropdown = document.querySelector("#settings-dropdown");
const adminLink = document.querySelector("#admin-link");

function closeSettingsMenu () {
    settingsDropdown.hidden = true;
    settingsToggle.setAttribute("aria-expanded", "false");
}

settingsToggle.addEventListener("click", () => {
    const willOpen = settingsDropdown.hidden;
    settingsDropdown.hidden = !willOpen;
    settingsToggle.setAttribute("aria-expanded", String(willOpen));
});

document.addEventListener("click", (event) => {
    if (!event.target.closest(".settings-menu")) {
        closeSettingsMenu();
    }
});

document.addEventListener("keydown", (event) => {
    if (event.key === "Escape") {
        closeSettingsMenu();
        settingsToggle.focus();
    }
});

taskDateInput.value = localDateString();
updateSelectedDayHeading(taskDateInput.value);
initializeDatePicker({
    input: taskDateInput,
    toggle: document.querySelector("#date-picker-toggle"),
    label: document.querySelector("#selected-date-label"),
    popover: document.querySelector("#date-picker-popover"),
    monthLabel: document.querySelector("#date-picker-month"),
    days: document.querySelector("#date-picker-days"),
    previousButton: document.querySelector("#date-picker-previous"),
    nextButton: document.querySelector("#date-picker-next"),
    todayButton: document.querySelector("#date-picker-today")
});
fetchTodos(taskDateInput.value);
fetchHabits(taskDateInput.value).catch((error) => {
    console.error("Failed to fetch habits:", error);
});

taskDateInput.addEventListener("change", () => {
    currentDate = dateTimeForSelectedDate(taskDateInput.value);
    updateSelectedDayHeading(taskDateInput.value);
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
            adminLink.hidden = false;
        }
    })
    .catch((error) => {
        console.error("Failed to load current user:", error);
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