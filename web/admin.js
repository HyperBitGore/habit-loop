import { createUser, deleteUser, editUser, getCurrentUser, getUsers } from "./users.js";

const userForm = document.querySelector("#user-form");
const userCreateError = document.querySelector("#user-create-error");
const userList = document.querySelector("#user-list");
const userListError = document.querySelector("#user-list-error");

function showError (element, error) {
    element.textContent = error.message;
    element.hidden = false;
}

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
        deleteButton.className = "danger-button";
        deleteButton.textContent = "Delete";
        deleteButton.addEventListener("click", async () => {
            if (!window.confirm(`Delete user "${user.name}"?`)) {
                return;
            }
            userListError.hidden = true;
            try {
                await deleteUser(user.id);
                await renderUsers();
            } catch (error) {
                showError(userListError, error);
            }
        });

        form.addEventListener("submit", async (event) => {
            event.preventDefault();
            userListError.hidden = true;
            try {
                await editUser(user.id, nameInput.value, roleSelect.value);
                await renderUsers();
            } catch (error) {
                showError(userListError, error);
            }
        });

        form.append(nameInput, roleSelect, saveButton, deleteButton);
        userList.appendChild(form);
    }
}

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
        await renderUsers();
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
        return renderUsers();
    })
    .catch((error) => {
        showError(userListError, error);
    });
