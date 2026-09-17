async function loadAds () {
    const response = await fetch("/api/app-config", { credentials: "same-origin" });
    if (!response.ok) return;
    const config = await response.json();
    const containers = document.querySelectorAll("[data-ad-slot]");
    if (config.adsense_test_placement) {
        for (const container of containers) {
            container.classList.add("ad-slot-test");
            container.textContent = "Ad placement";
        }
        return;
    }
    if (
        !config.adsense_publisher_id ||
        !config.adsense_ad_slot ||
        config.gpc_opt_out ||
        navigator.globalPrivacyControl === true
    ) return;

    window.google_non_personalized_ads = 1;
    const script = document.createElement("script");
    script.async = true;
    script.crossOrigin = "anonymous";
    script.src = `https://pagead2.googlesyndication.com/pagead/js/adsbygoogle.js?client=${encodeURIComponent(config.adsense_publisher_id)}`;
    script.onload = () => {
        for (const container of containers) {
            try {
                const ad = document.createElement("ins");
                ad.className = "adsbygoogle";
                ad.style.display = "block";
                ad.dataset.adClient = config.adsense_publisher_id;
                ad.dataset.adSlot = config.adsense_ad_slot;
                ad.dataset.adFormat = "auto";
                ad.dataset.fullWidthResponsive = "true";
                ad.dataset.npa = "1";
                container.replaceChildren(ad);
                (window.adsbygoogle = window.adsbygoogle || []).push({});
            } catch (error) {
                console.error("Failed to place an advertisement:", error);
                container.replaceChildren();
            }
        }
    };
    script.onerror = () => console.warn("AdSense was unavailable; continuing without ads.");
    document.head.append(script);
}

loadAds().catch((error) => console.error("Failed to load advertising configuration:", error));
