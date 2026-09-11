import { requestPasswordReset } from "./users.js";
import { createTurnstile, requireTurnstileToken } from "./turnstile.js";

const form = document.querySelector("#forgot-password-form");
const message = document.querySelector("#reset-message");
const turnstileWidget = createTurnstile(
    document.querySelector("#password-reset-turnstile"),
    "password_reset"
);
turnstileWidget.catch(() => {});

form.addEventListener("submit", async (event) => {
    event.preventDefault();
    message.hidden = true;
    try {
        const formData = new FormData(form);
        const widget = await turnstileWidget;
        await requestPasswordReset(
            formData.get("email"),
            requireTurnstileToken(widget)
        );
        widget.reset();
        message.textContent = "If an account uses that email, a reset link has been sent.";
        message.classList.add("is-success");
        message.hidden = false;
        form.reset();
    } catch (error) {
        turnstileWidget.then((widget) => widget.reset()).catch(() => {});
        message.classList.remove("is-success");
        message.textContent = error.message;
        message.hidden = false;
    }
});
