import { resetPassword } from "./users.js";

const form = document.querySelector("#reset-password-form");
const message = document.querySelector("#reset-message");
const token = new URLSearchParams(window.location.search).get("token");

form.addEventListener("submit", async (event) => {
    event.preventDefault();
    message.hidden = true;
    const formData = new FormData(form);
    const newPassword = formData.get("newPassword");
    if (!token) {
        message.textContent = "This password reset link is invalid.";
        message.hidden = false;
        return;
    }
    if (newPassword !== formData.get("confirmPassword")) {
        message.textContent = "Passwords do not match.";
        message.hidden = false;
        return;
    }
    try {
        await resetPassword(token, newPassword);
        message.textContent = "Password updated. You can now log in.";
        message.classList.add("is-success");
        message.hidden = false;
        form.reset();
    } catch (error) {
        message.classList.remove("is-success");
        message.textContent = error.message;
        message.hidden = false;
    }
});
