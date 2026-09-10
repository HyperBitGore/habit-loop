const monthFormatter = new Intl.DateTimeFormat(undefined, {
    month: "long",
    year: "numeric"
});

const selectedDateFormatter = new Intl.DateTimeFormat(undefined, {
    weekday: "short",
    month: "short",
    day: "numeric",
    year: "numeric"
});

function localDateString (date = new Date()) {
    const pad = (value) => String(value).padStart(2, "0");
    return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

function parseLocalDate (value) {
    const [year, month, day] = value.split("-").map(Number);
    return new Date(year, month - 1, day);
}

export function initializeDatePicker ({
    input,
    toggle,
    label,
    popover,
    monthLabel,
    days,
    previousButton,
    nextButton,
    todayButton
}) {
    let selectedDate = parseLocalDate(input.value || localDateString());
    let visibleMonth = new Date(selectedDate.getFullYear(), selectedDate.getMonth(), 1);

    function close () {
        popover.hidden = true;
        toggle.setAttribute("aria-expanded", "false");
    }

    function updateLabel () {
        label.textContent = selectedDateFormatter.format(selectedDate);
    }

    function selectDate (date) {
        selectedDate = date;
        visibleMonth = new Date(date.getFullYear(), date.getMonth(), 1);
        input.value = localDateString(date);
        updateLabel();
        render();
        close();
        input.dispatchEvent(new Event("change", { bubbles: true }));
        toggle.focus();
    }

    function render () {
        const year = visibleMonth.getFullYear();
        const month = visibleMonth.getMonth();
        const selectedValue = localDateString(selectedDate);
        const todayValue = localDateString();

        monthLabel.textContent = monthFormatter.format(visibleMonth);
        days.replaceChildren();

        const firstWeekday = new Date(year, month, 1).getDay();
        for (let index = 0; index < firstWeekday; index += 1) {
            const blank = document.createElement("span");
            blank.className = "date-picker-blank";
            blank.setAttribute("aria-hidden", "true");
            days.appendChild(blank);
        }

        const daysInMonth = new Date(year, month + 1, 0).getDate();
        for (let day = 1; day <= daysInMonth; day += 1) {
            const date = new Date(year, month, day);
            const value = localDateString(date);
            const button = document.createElement("button");
            button.type = "button";
            button.className = "date-picker-day";
            button.textContent = String(day);
            button.setAttribute("aria-label", selectedDateFormatter.format(date));
            button.setAttribute("aria-pressed", String(value === selectedValue));
            button.classList.toggle("is-selected", value === selectedValue);
            button.classList.toggle("is-today", value === todayValue);
            button.addEventListener("click", () => selectDate(date));
            days.appendChild(button);
        }
    }

    toggle.addEventListener("click", () => {
        const willOpen = popover.hidden;
        popover.hidden = !willOpen;
        toggle.setAttribute("aria-expanded", String(willOpen));
        if (willOpen) {
            visibleMonth = new Date(selectedDate.getFullYear(), selectedDate.getMonth(), 1);
            render();
        }
    });

    previousButton.addEventListener("click", () => {
        visibleMonth = new Date(visibleMonth.getFullYear(), visibleMonth.getMonth() - 1, 1);
        render();
    });

    nextButton.addEventListener("click", () => {
        visibleMonth = new Date(visibleMonth.getFullYear(), visibleMonth.getMonth() + 1, 1);
        render();
    });

    todayButton.addEventListener("click", () => {
        selectDate(new Date());
    });

    document.addEventListener("click", (event) => {
        if (!event.target.closest(".date-picker")) {
            close();
        }
    });

    document.addEventListener("keydown", (event) => {
        if (event.key === "Escape" && !popover.hidden) {
            close();
            toggle.focus();
        }
    });

    updateLabel();
    render();
}
