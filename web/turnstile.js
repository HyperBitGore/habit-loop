const siteKey = "0x4AAAAAAEvc5sHyX179V_1L";

function loadTurnstileAPI () {
    if (window.turnstile) {
        return Promise.resolve(window.turnstile);
    }
    const script = document.querySelector("#turnstile-script");
    if (!script) {
        return Promise.reject(new Error("Verification could not be loaded."));
    }
    return new Promise((resolve, reject) => {
        const timeout = window.setTimeout(() => {
            reject(new Error("Verification could not be loaded."));
        }, 10000);
        script.addEventListener("load", () => {
            window.clearTimeout(timeout);
            if (window.turnstile) {
                resolve(window.turnstile);
            } else {
                reject(new Error("Verification could not be loaded."));
            }
        }, { once: true });
        script.addEventListener("error", () => {
            window.clearTimeout(timeout);
            reject(new Error("Verification could not be loaded."));
        }, { once: true });
    });
}

export async function createTurnstile (element, action) {
    const turnstile = await loadTurnstileAPI();
    const widgetID = turnstile.render(element, {
        sitekey: siteKey,
        action,
        theme: "light",
        size: "flexible"
    });
    return {
        token () {
            return turnstile.getResponse(widgetID);
        },
        reset () {
            turnstile.reset(widgetID);
        }
    };
}

export function requireTurnstileToken (widget) {
    const token = widget.token();
    if (!token) {
        throw new Error("Complete the verification challenge.");
    }
    return token;
}
