import { requestPasswordReset } from "./users.js";

const form = document.querySelector("#forgot-password-form");
const message = document.querySelector("#reset-message");

form.addEventListener("submit", async (event) => {
    event.preventDefault();
    message.hidden = true;
    try {
        const formData = new FormData(form);
        await requestPasswordReset(formData.get("email"));
        message.textContent = "If an account uses that email, a reset link has been sent.";
        message.classList.add("is-success");
        message.hidden = false;
        form.reset();
    } catch (error) {
        message.classList.remove("is-success");
        message.textContent = error.message;
        message.hidden = false;
    }
});
