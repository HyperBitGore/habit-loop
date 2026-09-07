

export async function apiFetch (url, options = {}) {
     const response = await fetch(url, {
        ...options,
        credentials: "same-origin",
    });

    if (response.status === 401) {
        window.location.assign("/login.html");
        throw new Error("Authentication required");
    }

    return response;
}