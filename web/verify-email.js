const button = document.querySelector("#verify-email");
const message = document.querySelector("#verification-message");
const token = new URLSearchParams(window.location.search).get("token");

button.addEventListener("click", async () => {
    message.hidden = true;
    if (!token) {
        message.textContent = "This verification link is invalid.";
        message.hidden = false;
        return;
    }
    const response = await fetch("/api/verify-email", {
        method: "POST",
        headers: {
            "Content-Type": "application/json"
        },
        body: JSON.stringify({ token })
    });
    if (!response.ok) {
        let error = "Unable to verify email.";
        try {
            error = (await response.json()).error || error;
        } catch {
            // Keep the generic message.
        }
        message.textContent = error;
        message.hidden = false;
        return;
    }
    message.textContent = "Email verified. You can now log in.";
    message.classList.add("is-success");
    message.hidden = false;
    button.hidden = true;
});
