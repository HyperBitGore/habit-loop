async function loadSiteConfig() {
    const response = await fetch("/api/app-config");
    if (!response.ok) {
        throw new Error("Unable to load site configuration.");
    }

    const config = await response.json();
    if (typeof config.title !== "string" || config.title.trim() === "") {
        throw new Error("Site configuration did not include a title.");
    }

    const title = config.title.trim();
    document.title = document.title.replace("Habit Loop", title);
    for (const element of document.querySelectorAll("[data-site-title]")) {
        element.textContent = title;
    }
}

loadSiteConfig().catch((error) => {
    console.error("Failed to load site configuration:", error);
});
