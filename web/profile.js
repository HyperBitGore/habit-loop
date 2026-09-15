import { deleteAccount, getCurrentUser, logout, setPassword, updateProfile } from "./users.js";

const profileForm = document.querySelector("#profile-form");
const profileName = document.querySelector("#profile-name");
const profileEmail = document.querySelector("#profile-email");
const pendingEmail = document.querySelector("#pending-email");
const profileMessage = document.querySelector("#profile-message");
const passwordForm = document.querySelector("#password-form");
const passwordMessage = document.querySelector("#password-message");
const deleteAccountForm = document.querySelector("#delete-account-form");
const deleteAccountMessage = document.querySelector("#delete-account-message");

function showMessage(element, message, success = false) {
    element.textContent = message;
    element.classList.toggle("is-success", success);
    element.hidden = false;
}

getCurrentUser()
    .then((user) => {
        profileName.value = user.name;
        profileEmail.value = user.email;
        if (user.pending_email) {
            pendingEmail.textContent = `Pending verification: ${user.pending_email}`;
            pendingEmail.hidden = false;
        }
    })
    .catch((error) => {
        showMessage(profileMessage, error.message);
    });

profileForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    profileMessage.hidden = true;
    const formData = new FormData(profileForm);
    try {
        const result = await updateProfile(
            formData.get("name"),
            formData.get("email"),
            formData.get("currentPassword")
        );
        const message = result.email_verification_required
            ? "Profile updated. Check the new email address to confirm the change."
            : "Personal information updated.";
        showMessage(profileMessage, message, true);
        document.querySelector("#profile-current-password").value = "";
    } catch (error) {
        showMessage(profileMessage, error.message);
    }
});

passwordForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    passwordMessage.hidden = true;
    const formData = new FormData(passwordForm);
    const newPassword = formData.get("newPassword");
    if (newPassword !== formData.get("confirmPassword")) {
        showMessage(passwordMessage, "New passwords do not match.");
        return;
    }
    try {
        await setPassword(formData.get("currentPassword"), newPassword);
        window.location.assign("./login.html");
    } catch (error) {
        showMessage(passwordMessage, error.message);
    }
});

deleteAccountForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    deleteAccountMessage.hidden = true;
    const formData = new FormData(deleteAccountForm);
    if (!window.confirm("This permanently deletes your account and all of its data. Continue?")) {
        return;
    }
    try {
        await deleteAccount(formData.get("password"), formData.get("confirmation"));
        window.location.assign("./login.html");
    } catch (error) {
        showMessage(deleteAccountMessage, error.message);
    }
});

document.querySelector("#logout").addEventListener("click", logout);
