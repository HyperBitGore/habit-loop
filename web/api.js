

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

export async function reorderItems (type, ids, date = "") {
    const response = await apiFetch("/api/reorder", {
        method: "PUT",
        headers: {
            "Content-Type": "application/json"
        },
        body: JSON.stringify({ type, ids, date })
    });
    if (!response.ok) {
        throw new Error(`Unable to reorder ${type}. Status: ${response.status}`);
    }
}

export async function saveNote (type, id, date, body) {
    const response = await apiFetch("/api/notes", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ type, id, date, body })
    });
    if (!response.ok) throw new Error(`Unable to save note. Status: ${response.status}`);
}

export async function getNote (type, id, date) {
    const response = await apiFetch(`/api/get_note?type=${type}&id=${id}&date=${date}`);
    if (!response.ok) throw new Error(`Unable to load note. Status: ${response.status}`);
    return response.json();
}