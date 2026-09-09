const loginForm = document.querySelector("#login-form");
const registrationPopup = document.querySelector("#registration-popup");
const registrationForm = document.querySelector("#registration-form");
const registrationName = document.querySelector("#registration-name");
const openRegistrationButton = document.querySelector("#open-registration-button");
const closeRegistrationButton = document.querySelector("#close-registration-button");

function closeRegistrationPopup () {
    registrationPopup.hidden = true;
    registrationForm.reset();
    document.querySelector("#registration-error").hidden = true;
}

if (openRegistrationButton) {
    openRegistrationButton.addEventListener("click", () => {
        registrationPopup.hidden = false;
        registrationName.focus();
    });
}

if (closeRegistrationButton) {
    closeRegistrationButton.addEventListener("click", closeRegistrationPopup);
}

if (registrationPopup) {
    registrationPopup.addEventListener("click", (event) => {
        if (event.target === registrationPopup) {
            closeRegistrationPopup();
        }
    });
}

if (registrationForm) {
    registrationForm.addEventListener("submit", async (event) => {
        event.preventDefault();

        const formData = new FormData(registrationForm);
        const password = formData.get("password");
        const confirmation = formData.get("password-confirmation");
        const errorMessage = document.querySelector("#registration-error");
        errorMessage.hidden = true;

        if (password !== confirmation) {
            errorMessage.textContent = "Passwords do not match.";
            errorMessage.hidden = false;
            return;
        }

        try {
            await registerUser(formData.get("name"), formData.get("email"), password);
            closeRegistrationPopup();
            document.querySelector("#name").value = formData.get("name");
            const loginError = document.querySelector("#login-error");
            loginError.textContent = "Account created. You can now log in.";
            loginError.classList.add("is-success");
            loginError.hidden = false;
        } catch (error) {
            errorMessage.textContent = error.message;
            errorMessage.hidden = false;
        }
    });
}

if (loginForm) {
    loginForm.addEventListener("submit", async (event) => {
        event.preventDefault();

        const formData = new FormData(loginForm);
        const errorMessage = document.querySelector("#login-error");
        errorMessage.hidden = true;

        try {
            await login(formData.get("name"), formData.get("password"));
            window.location.assign("./todo.html");
        } catch (error) {
            errorMessage.textContent = error.message;
            errorMessage.hidden = false;
        }
    });
}

export async function login (username, password) {
    const response = await fetch("/api/login", {
        method: "POST",
        headers: {
            "Content-Type": "application/json"
        },
        body: JSON.stringify({
            name: username,
            password: password
        })
    });

    if (!response.ok) {
        throw new Error("Invalid login credentials.");
    }
}
export async function logout () {
    try {
        await fetch("/api/logout", { method: "POST" });
    } finally {
        window.location.assign("./login.html");
    }
}

export async function registerUser (name, email, password) {
    const response = await fetch("/api/create_account", {
        method: "POST",
        headers: {
            "Content-Type": "application/json"
        },
        body: JSON.stringify({
            name,
            email,
            password
        })
    });

    if (!response.ok) {
        const message = await response.text();
        throw new Error(message || "Unable to register user.");
    }
}

export async function createUser (name, password, role) {
    const response = await fetch("/api/register_user", {
        method: "PUT",
        headers: {
            "Content-Type": "application/json"
        },
        body: JSON.stringify({ name, password, role })
    });
    if (!response.ok) {
        throw new Error(await response.text() || "Unable to create user.");
    }
}

export async function getCurrentUser () {
    const response = await fetch("/api/current_user");
    if (!response.ok) {
        throw new Error("Unable to determine the current user.");
    }
    return response.json();
}

export async function getUsers () {
    const response = await fetch("/api/get_users");
    if (!response.ok) {
        throw new Error("Unable to load users.");
    }
    return response.json();
}

export async function editUser (id, name, role) {
    const response = await fetch("/api/edit_user", {
        method: "PUT",
        headers: {
            "Content-Type": "application/json"
        },
        body: JSON.stringify({ id, name, role })
    });
    if (!response.ok) {
        throw new Error("Unable to update user.");
    }
}

export async function deleteUser (id, name) {
    const response = await fetch("/api/delete_user", {
        method: "PUT",
        headers: {
            "Content-Type": "application/json"
        },
        body: JSON.stringify({ id, name })
    });
    if (!response.ok) {
        throw new Error(await response.text() || "Unable to delete user.");
    }
}

export async function setPassword (currentPassword, newPassword) {
    const response = await fetch("/api/set_password", {
        method: "PUT",
        headers: {
            "Content-Type": "application/json"
        },
        body: JSON.stringify({
            current_password: currentPassword,
            new_password: newPassword
        })
    });
    if (!response.ok) {
        throw new Error(await response.text() || "Unable to update password.");
    }
}