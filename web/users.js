const loginForm = document.querySelector("#login-form");

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