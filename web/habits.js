import { apiFetch, getNote, reorderItems, saveNote } from "./api.js";

let detailHabit = null;
let calendarMonth = new Date();
let habitArray = [];

function localDateString (date = new Date()) {
    const pad = (value) => String(value).padStart(2, "0");
    return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

function shiftDate (dateValue, days) {
    const [year, month, day] = dateValue.split("-").map(Number);
    const date = new Date(Date.UTC(year, month - 1, day));
    date.setUTCDate(date.getUTCDate() + days);
    return date.toISOString().slice(0, 10);
}

function calculateHabitStats (completions, skips) {
    const completionDates = new Set(
        completions.map((completion) => completion.slice(0, 10))
    );
    const skippedDates = new Set(
        skips.map((skip) => skip.slice(0, 10))
    );
    const streakDates = new Set([...completionDates, ...skippedDates]);
    const sortedDates = [...streakDates].sort();

    let longestStreak = 0;
    let runningStreak = 0;
    let previousDate = null;
    for (const date of sortedDates) {
        runningStreak = previousDate && shiftDate(previousDate, 1) === date
            ? runningStreak + 1
            : 1;
        longestStreak = Math.max(longestStreak, runningStreak);
        previousDate = date;
    }

    const today = localDateString();
    const yesterday = shiftDate(today, -1);
    let streakDate = streakDates.has(today)
        ? today
        : streakDates.has(yesterday) ? yesterday : null;
    let currentStreak = 0;
    while (streakDate && streakDates.has(streakDate)) {
        currentStreak += 1;
        streakDate = shiftDate(streakDate, -1);
    }

    return {
        totalCompletions: completionDates.size,
        totalSkips: skippedDates.size,
        currentStreak,
        longestStreak
    };
}

function formatDays (days) {
    return `${days} ${days === 1 ? "day" : "days"}`;
}

export function shouldDisplayHabit (habit, date) {
    if (habit.days_mode) {
        const weekday = [
            "sunday",
            "monday",
            "tuesday",
            "wednesday",
            "thursday",
            "friday",
            "saturday"
        ][new Date(`${date}T00:00:00Z`).getUTCDay()];
        return Boolean(habit.days_of_week?.[weekday]);
    }

    const interval = Math.max(1, Number(habit.interval) || 1);
    const startDate = habit.start_date || date;
    const selectedTime = Date.parse(`${date}T00:00:00Z`);
    const startTime = Date.parse(`${startDate}T00:00:00Z`);
    const elapsedDays = Math.round((selectedTime - startTime) / 86400000);
    return elapsedDays >= 0 && elapsedDays % interval === 0;
}

function renderHabitCalendar () {
    if (!detailHabit) {
        return;
    }
    const year = calendarMonth.getFullYear();
    const month = calendarMonth.getMonth();
    const completedDates = new Set(
        (detailHabit.completions ?? []).map((completion) => completion.slice(0, 10))
    );
    const skippedDates = new Set(
        (detailHabit.skips ?? []).map((skip) => skip.slice(0, 10))
    );
    const today = localDateString();
    const calendarDays = document.querySelector("#habit-calendar-days");
    calendarDays.replaceChildren();

    document.querySelector("#habit-calendar-month").textContent =
        new Intl.DateTimeFormat(undefined, { month: "long", year: "numeric" })
            .format(calendarMonth);

    const firstWeekday = new Date(year, month, 1).getDay();
    for (let index = 0; index < firstWeekday; index += 1) {
        const blank = document.createElement("span");
        blank.className = "habit-calendar-blank";
        blank.setAttribute("aria-hidden", "true");
        calendarDays.appendChild(blank);
    }

    const daysInMonth = new Date(year, month + 1, 0).getDate();
    for (let day = 1; day <= daysInMonth; day += 1) {
        const dateValue = localDateString(new Date(year, month, day));
        const dayElement = document.createElement("span");
        dayElement.className = "habit-calendar-day";
        dayElement.textContent = String(day);
        if (completedDates.has(dateValue)) {
            dayElement.classList.add("is-complete");
            dayElement.setAttribute("aria-label", `${dateValue}, completed`);
        } else if (skippedDates.has(dateValue)) {
            dayElement.classList.add("is-skipped");
            dayElement.setAttribute("aria-label", `${dateValue}, skipped`);
        } else if (!shouldDisplayHabit(detailHabit, dateValue)) {
            dayElement.classList.add("is-not-targeted");
            dayElement.setAttribute("aria-label", `${dateValue}, not scheduled`);
        } else {
            dayElement.setAttribute("aria-label", `${dateValue}, scheduled`);
        }
        if (dateValue === today) {
            dayElement.classList.add("is-today");
        }
        calendarDays.appendChild(dayElement);
    }
}

export function changeHabitCalendarMonth (offset) {
    calendarMonth = new Date(
        calendarMonth.getFullYear(),
        calendarMonth.getMonth() + offset,
        1
    );
    renderHabitCalendar();
}

async function openHabitDetail (habit, date) {
    detailHabit = habit;
    const now = new Date();
    calendarMonth = new Date(now.getFullYear(), now.getMonth(), 1);
    const stats = calculateHabitStats(habit.completions ?? [], habit.skips ?? []);
    document.querySelector("#habit-detail-name").value = habit.name;
    const scheduleMode = document.querySelector("#habit-detail-schedule-mode");
    scheduleMode.value = habit.days_mode ? "days" : "interval";
    document.querySelector("#habit-detail-interval").value = Math.max(1, Number(habit.interval) || 1);
    for (const checkbox of document.querySelectorAll("#habit-detail-weekday-fields input[data-day]")) {
        checkbox.checked = Boolean(habit.days_of_week?.[checkbox.dataset.day]);
    }
    scheduleMode.dispatchEvent(new Event("change"));
    document.querySelector("#habit-total-completions").textContent =
        formatDays(stats.totalCompletions);
    document.querySelector("#habit-current-streak").textContent =
        formatDays(stats.currentStreak);
    document.querySelector("#habit-longest-streak").textContent =
        formatDays(stats.longestStreak);
    document.querySelector("#habit-total-skips").textContent =
        formatDays(stats.totalSkips);
    document.querySelector("#habit-metric-name").value = habit.metric_name || "";
    document.querySelector("#habit-metric-goal").value = habit.metric_goal || "";
    setMetricMode(Boolean(habit.metric_name));
    document.querySelector("#habit-detail-note").value =
        (await getNote("habit", habit.id, date)).body || "";
    renderHabitCalendar();
    document.querySelector("#habit-detail-error").hidden = true;
    document.querySelector("#habit-detail-popup").hidden = false;
}

export function closeHabitDetail () {
    detailHabit = null;
    document.querySelector("#habit-detail-popup").hidden = true;
}

function renderHabits (habits, date) {
    const habitList = document.querySelector("#habit-list");
    habitList.replaceChildren();
    habitArray = habits;
    for (const habit of habits.filter((habit) => shouldDisplayHabit(habit, date))) {
        const listItem = document.createElement("li");
        listItem.draggable = true;
        listItem.dataset.id = String(habit.id);
        listItem.addEventListener("dragover", (event) => event.preventDefault());
        listItem.addEventListener("drop", async (event) => {
            event.preventDefault();
            const draggedID = event.dataTransfer.getData("text/plain");
            const targetID = String(habit.id);
            if (!draggedID || draggedID === targetID) {
                return;
            }
            const visibleIDs = habits
                .filter((item) => shouldDisplayHabit(item, date))
                .map((item) => item.id);
            const from = visibleIDs.indexOf(Number(draggedID));
            const to = visibleIDs.indexOf(Number(targetID));
            [visibleIDs[from], visibleIDs[to]] = [visibleIDs[to], visibleIDs[from]];
            reorderVisibleHabits(visibleIDs, date);
        });
        listItem.addEventListener("dragstart", (event) => {
            event.dataTransfer.setData("text/plain", String(habit.id));
        });

        const habitName = document.createElement("span");
        habitName.className = "task-name";
        habitName.textContent = habit.name;

        const completions = habit.completions ?? [];
        const skips = habit.skips ?? [];
        const isComplete = completions.some(
            (completion) => completion.slice(0, 10) === date
        );
        const isSkipped = skips.some((skip) => skip.slice(0, 10) === date);
        const completeButton = document.createElement(habit.metric_name ? "span" : "button");
        completeButton.className = habit.metric_name ? "metric-status" : "status-button";
        if (habit.metric_name) {
            completeButton.textContent = `${habit.metric_value} / ${habit.metric_goal}`;
            completeButton.classList.toggle("is-complete", isComplete);
            completeButton.setAttribute("role", "button");
            completeButton.tabIndex = 0;
            completeButton.addEventListener("click", (event) => {
                event.stopPropagation();
                window.dispatchEvent(new CustomEvent("habit-metric-edit", {
                    detail: { habit, date }
                }));
            });
        } else {
            completeButton.type = "button";
            completeButton.textContent = isComplete ? "✓" : "×";
            completeButton.classList.toggle("is-complete", isComplete);
            completeButton.setAttribute(
                "aria-label",
                isComplete ? `Mark ${habit.name} incomplete for ${date}` : `Complete ${habit.name} for ${date}`
            );
            completeButton.addEventListener("click", async (event) => {
                event.stopPropagation();
                if (isComplete) {
                    await uncompleteHabit(habit.id, date);
                } else {
                    await completeHabit(habit.id, date);
                }
                await fetchHabits(date);
            });
        }

        const skipButton = document.createElement("button");
        skipButton.type = "button";
        skipButton.className = "status-button skip-button";
        skipButton.textContent = "→";
        skipButton.classList.toggle("is-skipped", isSkipped);
        skipButton.setAttribute(
            "aria-label",
            isSkipped ? `Unskip ${habit.name} for ${date}` : `Skip ${habit.name} for ${date}`
        );
        skipButton.addEventListener("click", async (event) => {
            event.stopPropagation();
            if (isSkipped) {
                await unskipHabit(habit.id, date);
            } else {
                await skipHabit(habit.id, date);
            }
            await fetchHabits(date);
        });

        const habitActions = document.createElement("span");
        habitActions.className = "habit-actions";
        habitActions.append(
            createMoveButton(habit, date, -1),
            createMoveButton(habit, date, 1),
            completeButton,
            skipButton
        );

        listItem.addEventListener("click", () => openHabitDetail(habit, date));
        listItem.append(habitName, habitActions);
        habitList.appendChild(listItem);
    }
}

function createMoveButton (habit, date, direction) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "status-button reorder-button";
    button.textContent = direction < 0 ? "↑" : "↓";
    button.setAttribute("aria-label", `Move ${habit.name} ${direction < 0 ? "up" : "down"}`);
    button.addEventListener("click", async (event) => {
        event.stopPropagation();
        const visible = habitArray.filter((item) => shouldDisplayHabit(item, date));
        const index = visible.indexOf(habit);
        const next = index + direction;
        if (next < 0 || next >= visible.length) {
            return;
        }
        [visible[index], visible[next]] = [visible[next], visible[index]];
        reorderVisibleHabits(visible.map((item) => item.id), date);
    });
    return button;
}

function reorderVisibleHabits (visibleIDs, date) {
    const visibleSet = new Set(visibleIDs);
    const habitsByID = new Map(habitArray.map((habit) => [habit.id, habit]));
    let index = 0;
    habitArray = habitArray.map((habit) => {
        if (!visibleSet.has(habit.id)) {
            return habit;
        }
        return habitsByID.get(visibleIDs[index++]);
    });
    renderHabits(habitArray, date);
    reorderItems("habits", habitArray.map((habit) => habit.id)).catch((error) => {
        console.error("Failed to save habit order:", error);
    });
}

export async function fetchHabits (date) {
    const response = await apiFetch(`/api/get_habits?date=${encodeURIComponent(date)}`);
    if (!response.ok) {
        throw new Error(`Unable to fetch habits. Status: ${response.status}`);
    }

    const habits = await response.json();
    renderHabits(habits, date);
}

export async function saveSelectedHabitExtras (date) {
    if (!detailHabit) return;
    const response = await apiFetch("/api/save_metric", {
        method: "PUT",
        headers: {"Content-Type": "application/json"},
        body: JSON.stringify({
            habit_id: detailHabit.id,
            name: document.querySelector("#habit-metric-name").value,
            goal: Number(document.querySelector("#habit-metric-goal").value),
            date
        })
    });
    if (!response.ok) throw new Error(`Unable to save metric. Status: ${response.status}`);
    await saveNote("habit", detailHabit.id, date, document.querySelector("#habit-detail-note").value);
}

export async function saveHabitMetricValue (habit, date, value) {
    const response = await apiFetch("/api/save_metric", {
        method: "PUT",
        headers: {"Content-Type": "application/json"},
        body: JSON.stringify({
            habit_id: habit.id,
            name: habit.metric_name,
            goal: habit.metric_goal,
            value,
            date
        })
    });
    if (!response.ok) throw new Error(`Unable to save metric. Status: ${response.status}`);
}

export function setMetricMode (enabled) {
    const fields = document.querySelector("#habit-metric-fields");
    const toggle = document.querySelector("#habit-metric-toggle");
    fields.hidden = !enabled;
    toggle.textContent = enabled ? "On" : "Off";
    toggle.setAttribute("aria-pressed", String(enabled));
}

export async function completeHabit (habitID, date) {
    const response = await apiFetch("/api/complete_habit", {
        method: "POST",
        headers: {
            "Content-Type": "application/json"
        },
        body: JSON.stringify({
            habit_id: habitID,
            date
        })
    });

    if (!response.ok) {
        throw new Error(`Unable to complete habit. Status: ${response.status}`);
    }
}

export async function uncompleteHabit (habitID, date) {
    const response = await apiFetch("/api/uncomplete_habit", {
        method: "POST",
        headers: {
            "Content-Type": "application/json"
        },
        body: JSON.stringify({
            habit_id: habitID,
            date
        })
    });

    if (!response.ok) {
        throw new Error(`Unable to uncomplete habit. Status: ${response.status}`);
    }
}

export async function skipHabit (habitID, date) {
    const response = await apiFetch("/api/skip_habit", {
        method: "POST",
        headers: {
            "Content-Type": "application/json"
        },
        body: JSON.stringify({
            habit_id: habitID,
            date
        })
    });

    if (!response.ok) {
        throw new Error(`Unable to skip habit. Status: ${response.status}`);
    }
}

export async function unskipHabit (habitID, date) {
    const response = await apiFetch("/api/unskip_habit", {
        method: "POST",
        headers: {
            "Content-Type": "application/json"
        },
        body: JSON.stringify({
            habit_id: habitID,
            date
        })
    });

    if (!response.ok) {
        throw new Error(`Unable to unskip habit. Status: ${response.status}`);
    }
}

function scheduleHeaders (schedule) {
    return {
        "X-Habit-Interval": String(schedule.interval),
        "X-Habit-Days-Mode": String(schedule.daysMode),
        "X-Habit-Days-Of-Week": JSON.stringify(schedule.daysOfWeek)
    };
}

export async function addHabit (name, schedule, startDate) {
    const response = await apiFetch("/api/add_habit", {
        method: "PUT",
        headers: {
            "X-Habit-Name": name,
            "X-Habit-Completions": JSON.stringify([]),
            "X-Habit-Start-Date": startDate,
            ...scheduleHeaders(schedule)
        }
    });

    if (!response.ok) {
        throw new Error(`Unable to add habit. Status: ${response.status}`);
    }
}

export async function saveSelectedHabit (name, date, schedule) {
    if (!detailHabit) {
        return;
    }
    const response = await apiFetch("/api/edit_habit", {
        method: "PUT",
        headers: {
            "X-Habit-ID": String(detailHabit.id),
            "X-Habit-Name": name,
            "X-Habit-Completions": JSON.stringify(
                !detailHabit.metric_name &&
                document.querySelector("#habit-metric-name").value.trim()
                    ? []
                    : detailHabit.completions ?? []
            ),
            ...scheduleHeaders(schedule)
        }
    });
    if (!response.ok) {
        throw new Error(`Unable to update habit. Status: ${response.status}`);
    }
    closeHabitDetail();
    await fetchHabits(date);
}

export async function deleteSelectedHabit (date) {
    if (!detailHabit) {
        return;
    }
    const response = await apiFetch("/api/delete_habit", {
        method: "PUT",
        headers: {
            "X-Habit-ID": String(detailHabit.id)
        }
    });
    if (!response.ok) {
        throw new Error(`Unable to delete habit. Status: ${response.status}`);
    }
    closeHabitDetail();
    await fetchHabits(date);
}