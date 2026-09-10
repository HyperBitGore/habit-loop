import { apiFetch } from "./api.js";

let detailHabit = null;
let calendarMonth = new Date();

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
        } else {
            dayElement.setAttribute("aria-label", dateValue);
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

function openHabitDetail (habit) {
    detailHabit = habit;
    const now = new Date();
    calendarMonth = new Date(now.getFullYear(), now.getMonth(), 1);
    const stats = calculateHabitStats(habit.completions ?? [], habit.skips ?? []);
    document.querySelector("#habit-detail-name").value = habit.name;
    document.querySelector("#habit-total-completions").textContent =
        formatDays(stats.totalCompletions);
    document.querySelector("#habit-current-streak").textContent =
        formatDays(stats.currentStreak);
    document.querySelector("#habit-longest-streak").textContent =
        formatDays(stats.longestStreak);
    document.querySelector("#habit-total-skips").textContent =
        formatDays(stats.totalSkips);
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
    for (const habit of habits) {
        const listItem = document.createElement("li");

        const habitName = document.createElement("span");
        habitName.className = "task-name";
        habitName.textContent = habit.name;

        const completions = habit.completions ?? [];
        const skips = habit.skips ?? [];
        const isComplete = completions.some(
            (completion) => completion.slice(0, 10) === date
        );
        const isSkipped = skips.some((skip) => skip.slice(0, 10) === date);
        const completeButton = document.createElement("button");
        completeButton.type = "button";
        completeButton.className = "status-button";
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
        habitActions.append(completeButton, skipButton);

        listItem.addEventListener("click", () => openHabitDetail(habit));
        listItem.append(habitName, habitActions);
        habitList.appendChild(listItem);
    }
}

export async function fetchHabits (date) {
    const response = await apiFetch("/api/get_habits");
    if (!response.ok) {
        throw new Error(`Unable to fetch habits. Status: ${response.status}`);
    }
    const habits = await response.json();
    renderHabits(habits, date);
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

export async function addHabit (name) {
    const response = await apiFetch("/api/add_habit", {
        method: "PUT",
        headers: {
            "X-Habit-Name": name,
            "X-Habit-Completions": JSON.stringify([])
        }
    });

    if (!response.ok) {
        throw new Error(`Unable to add habit. Status: ${response.status}`);
    }
}

export async function saveSelectedHabit (name, date) {
    if (!detailHabit) {
        return;
    }
    const response = await apiFetch("/api/edit_habit", {
        method: "PUT",
        headers: {
            "X-Habit-ID": String(detailHabit.id),
            "X-Habit-Name": name,
            "X-Habit-Completions": JSON.stringify(detailHabit.completions ?? [])
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