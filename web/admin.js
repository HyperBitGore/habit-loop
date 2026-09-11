import { createUser, deleteUser, editUser, getCurrentUser, getUsers } from "./users.js";

const userForm = document.querySelector("#user-form");
const userCreateError = document.querySelector("#user-create-error");
const userSearch = document.querySelector("#user-search");
const userList = document.querySelector("#user-list");
const userListError = document.querySelector("#user-list-error");
const userListEmpty = document.querySelector("#user-list-empty");
const userListLoading = document.querySelector("#user-list-loading");
const userListSentinel = document.querySelector("#user-list-sentinel");

let nextCursor = "";
let hasMoreUsers = true;
let loadingUsers = false;
let requestGeneration = 0;
let requestController = null;
let searchTimer = null;
let authorized = false;

function showError (element, error) {
    element.textContent = error.message;
    element.hidden = false;
}

function createUserRow (user) {
    const form = document.createElement("form");
    form.className = "user-list-row";

    const nameInput = document.createElement("input");
    nameInput.type = "text";
    nameInput.value = user.name;
    nameInput.setAttribute("aria-label", `Username for ${user.name}`);
    nameInput.required = true;

    const email = document.createElement("span");
    email.className = "user-list-email";
    email.textContent = user.email || "No email";
    email.title = user.email || "No email";

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
    deleteButton.className = "danger-button";
    deleteButton.textContent = "Delete";
    deleteButton.addEventListener("click", async () => {
        if (!window.confirm(`Delete user "${user.name}"?`)) {
            return;
        }
        userListError.hidden = true;
        try {
            await deleteUser(user.id);
            await resetUsers();
        } catch (error) {
            showError(userListError, error);
        }
    });

    form.addEventListener("submit", async (event) => {
        event.preventDefault();
        userListError.hidden = true;
        try {
            await editUser(user.id, nameInput.value, roleSelect.value);
            await resetUsers();
        } catch (error) {
            showError(userListError, error);
        }
    });

    form.append(nameInput, email, roleSelect, saveButton, deleteButton);
    return form;
}

async function loadNextUsers (generation = requestGeneration) {
    if (!authorized || loadingUsers || !hasMoreUsers || generation !== requestGeneration) {
        return;
    }

    loadingUsers = true;
    userListLoading.hidden = false;
    userListError.hidden = true;
    requestController = new AbortController();

    try {
        const page = await getUsers({
            search: userSearch.value.trim(),
            cursor: nextCursor,
            signal: requestController.signal
        });
        if (generation !== requestGeneration) {
            return;
        }
        for (const user of page.users) {
            userList.appendChild(createUserRow(user));
        }
        nextCursor = page.next_cursor || "";
        hasMoreUsers = Boolean(nextCursor);
        userListEmpty.hidden = userList.childElementCount !== 0;
        userListSentinel.hidden = !hasMoreUsers;
    } catch (error) {
        if (error.name !== "AbortError" && generation === requestGeneration) {
            showError(userListError, error);
        }
    } finally {
        if (generation === requestGeneration) {
            loadingUsers = false;
            userListLoading.hidden = true;
            requestController = null;
        }
    }
}

async function resetUsers () {
    requestGeneration += 1;
    requestController?.abort();
    loadingUsers = false;
    nextCursor = "";
    hasMoreUsers = true;
    userList.replaceChildren();
    userListEmpty.hidden = true;
    userListSentinel.hidden = false;
    await loadNextUsers(requestGeneration);
}

const userListObserver = new IntersectionObserver((entries) => {
    if (entries.some((entry) => entry.isIntersecting)) {
        loadNextUsers();
    }
}, {
    rootMargin: "300px 0px"
});
userListObserver.observe(userListSentinel);

userSearch.addEventListener("input", () => {
    window.clearTimeout(searchTimer);
    searchTimer = window.setTimeout(resetUsers, 250);
});

userForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    userCreateError.hidden = true;
    const formData = new FormData(userForm);
    try {
        await createUser(
            formData.get("name"),
            formData.get("email"),
            formData.get("password"),
            formData.get("role")
        );
        userForm.reset();
        await resetUsers();
    } catch (error) {
        showError(userCreateError, error);
    }
});

getCurrentUser()
    .then((user) => {
        if (user.role !== "admin") {
            window.location.replace("./todo.html");
            return;
        }
        authorized = true;
        return resetUsers();
    })
    .catch((error) => {
        showError(userListError, error);
    });
