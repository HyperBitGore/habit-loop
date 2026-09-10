import { getCurrentUser, logout, setPassword, updateProfile } from "./users.js";

const profileForm = document.querySelector("#profile-form");
const profileName = document.querySelector("#profile-name");
const profileEmail = document.querySelector("#profile-email");
const profileMessage = document.querySelector("#profile-message");
const passwordForm = document.querySelector("#password-form");
const passwordMessage = document.querySelector("#password-message");

function showMessage(element, message, success = false) {
    element.textContent = message;
    element.classList.toggle("is-success", success);
    element.hidden = false;
}

getCurrentUser()
    .then((user) => {
        profileName.value = user.name;
        profileEmail.value = user.email;
    })
    .catch((error) => {
        showMessage(profileMessage, error.message);
    });

profileForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    profileMessage.hidden = true;
    const formData = new FormData(profileForm);
    try {
        await updateProfile(formData.get("name"), formData.get("email"));
        showMessage(profileMessage, "Personal information updated.", true);
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
        passwordForm.reset();
        showMessage(passwordMessage, "Password updated.", true);
    } catch (error) {
        showMessage(passwordMessage, error.message);
    }
});

document.querySelector("#logout").addEventListener("click", logout);
