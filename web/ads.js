async function loadAds () {
    const response = await fetch("/api/app-config", {
        credentials: "same-origin",
        cache: "no-store"
    });
    if (!response.ok) {
        throw new Error(`Advertising configuration returned HTTP ${response.status}.`);
    }
    const config = await response.json();
    let containers = [...document.querySelectorAll("[data-ad-slot]")];
    if (!config.adsense_authed_pages) {
        for (const container of containers) {
            if (container.dataset.adAuthed !== undefined) {
                container.remove();
            }
        }
        containers = containers.filter((container) => container.isConnected);
    }
    if (containers.length === 0) return;

    if (config.adsense_test_placement) {
        for (const container of containers) {
            container.classList.add("ad-slot-test");
            container.textContent = "Ad placement";
        }
        return;
    }
    if (
        !config.adsense_publisher_id ||
        !config.adsense_ad_slot
    ) {
        console.info("AdSense not loaded: publisher or slot is not configured.");
        return;
    }

    // When a certified CMP (e.g. the AdSense GDPR message) is active, consent
    // signals govern personalization; this flag additionally forces
    // non-personalized ads everywhere when enabled.
    const nonPersonalized = config.adsense_non_personalized !== false;
    if (nonPersonalized) {
        window.google_non_personalized_ads = 1;
    }
    const scriptURL = new URL("https://pagead2.googlesyndication.com/pagead/js/adsbygoogle.js");
    scriptURL.searchParams.set("client", config.adsense_publisher_id);
    let script = document.querySelector(`script[src="${scriptURL}"]`);
    if (!script) {
        script = document.createElement("script");
        script.async = true;
        script.nonce = document.querySelector("script[nonce]")?.nonce || "";
        script.src = scriptURL;
    }

    await new Promise((resolve, reject) => {
        if (script.dataset.loaded === "true") {
            resolve();
            return;
        }
        script.addEventListener("load", () => {
            script.dataset.loaded = "true";
            resolve();
        }, { once: true });
        script.addEventListener("error", () => reject(new Error("The AdSense script could not be loaded.")), { once: true });
        if (!script.isConnected) document.head.append(script);
    });

    for (const container of containers) {
        try {
            const ad = document.createElement("ins");
            ad.className = "adsbygoogle";
            ad.style.display = "block";
            ad.dataset.adClient = config.adsense_publisher_id;
            ad.dataset.adSlot = config.adsense_ad_slot;
            ad.dataset.adFormat = "auto";
            ad.dataset.fullWidthResponsive = "true";
            if (nonPersonalized) {
                ad.dataset.npa = "1";
            }
            container.replaceChildren(ad);
            (window.adsbygoogle = window.adsbygoogle || []).push({});
        } catch (error) {
            console.error("Failed to place an advertisement:", error);
            container.replaceChildren();
        }
    }
}

loadAds().catch((error) => console.warn("AdSense was unavailable; continuing without ads.", error));
