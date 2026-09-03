
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
        method: "GET",
        headers: {
            "X-User-Name": username,
            "X-User-Password": password
        }
    });

    if (!response.ok) {
        throw new Error("Invalid login credentials.");
    }
}