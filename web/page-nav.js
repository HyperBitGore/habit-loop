import { getCurrentUser, logout } from "./users.js";

const settingsToggle = document.querySelector("#settings-toggle");
const settingsDropdown = document.querySelector("#settings-dropdown");
const adminLink = document.querySelector("#admin-link");
const logoutButton = document.querySelector("#logout");

if (settingsToggle && settingsDropdown) {
    settingsToggle.addEventListener("click", () => {
        const willOpen = settingsDropdown.hidden;
        settingsDropdown.hidden = !willOpen;
        settingsToggle.setAttribute("aria-expanded", String(willOpen));
    });

    document.addEventListener("click", (event) => {
        if (!event.target.closest(".settings-menu")) {
            settingsDropdown.hidden = true;
            settingsToggle.setAttribute("aria-expanded", "false");
        }
    });
}

if (logoutButton) {
    logoutButton.addEventListener("click", logout);
}

if (adminLink) {
    getCurrentUser()
        .then((user) => {
            adminLink.hidden = user.role !== "admin";
        })
        .catch((error) => console.error("Failed to load current user:", error));
}
