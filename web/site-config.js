async function loadSiteConfig() {
    const response = await fetch("/api/app-config");
    if (!response.ok) {
        throw new Error("Unable to load site configuration.");
    }

    const config = await response.json();
    if (typeof config.title !== "string" || config.title.trim() === "") {
        throw new Error("Site configuration did not include a title.");
    }
    if (typeof config.contact_email !== "string" || config.contact_email.trim() === "") {
        throw new Error("Site configuration did not include a contact email.");
    }

    const title = config.title.trim();
    const contactEmail = config.contact_email.trim();
    document.title = document.title.replace("Habit Loop", title);
    for (const element of document.querySelectorAll("[data-site-title]")) {
        element.textContent = title;
    }
    for (const element of document.querySelectorAll("[data-contact-email]")) {
        element.textContent = contactEmail;
        if (element instanceof HTMLAnchorElement) {
            element.href = `mailto:${contactEmail}`;
        }
    }
}

loadSiteConfig().catch((error) => {
    console.error("Failed to load site configuration:", error);
});
