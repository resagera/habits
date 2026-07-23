const tg = window.Telegram.WebApp;
const userId = tg.initDataUnsafe?.user?.id || "testuser";

const WEEKS_TO_SHOW = 7;
const DAYS_PER_WEEK = 7;

let categories = [];
const today = new Date();
let marks = [];

const dayNames = ["Пн", "Вт", "Ср", "Чт", "Пт", "Сб", "Вс"];

const trackerTab = document.getElementById("trackerTab");
const checksTab = document.getElementById("checksTab");
const tabTrackerBtn = document.getElementById("tab-tracker");
const tabChecksBtn = document.getElementById("tab-checks");

function generateRandomString(length) {
    const characters = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
    let result = '';
    const charactersLength = characters.length;
    for (let i = 0; i < length; i++) {
        result += characters.charAt(Math.floor(Math.random() * charactersLength));
    }
    return result;
}

// Переключение вкладок
tabTrackerBtn.onclick = () => {
    trackerTab.classList.remove("hidden");
    checksTab.classList.add("hidden");
    tabTrackerBtn.classList.add("border-blue-500", "font-semibold");
    tabChecksBtn.classList.remove("border-blue-500", "font-semibold");
};

tabChecksBtn.onclick = () => {
    checksTab.classList.remove("hidden");
    trackerTab.classList.add("hidden");
    tabChecksBtn.classList.add("border-blue-500", "font-semibold");
    tabTrackerBtn.classList.remove("border-blue-500", "font-semibold");
};

const pageMain = document.getElementById("page-main");
const pageSettingsCategory = document.getElementById("page-category-settings");
const pageSettingsGroup = document.getElementById("page-group-settings");

const saveBtn = document.getElementById("save");

saveBtn.onclick = (e) => {
    pageSettingsCategory.style.display = "none";
    pageSettingsGroup.style.display = "none";
    pageMain.style.display = "block";
};

// --- settings ---
const settingsCategoryNameInput = document.getElementById("category-name");
const settingsCategoryColorInput = document.getElementById("category-color");

settingsCategoryColorInput.onchange = (e) => { //oninput onchange
    const cat = e.target.getAttribute("category-name");
    const p = document.createElement("p"); p.textContent = '__'+cat; document.body.appendChild(p);
    localStorage.setItem(`color:${cat}`, e.target.value);
    render();
};

// --- helpers ---
function formatDate(d) {
    const dd = String(d.getDate()).padStart(2, "0");
    const mm = String(d.getMonth() + 1).padStart(2, "0");
    const yyyy = d.getFullYear();
    return `${dd}.${mm}.${yyyy}`;
}

function getWeekStart(d) {
    const dt = new Date(d);
    const day = dt.getDay();
    const diff = (day === 0) ? -6 : (1 - day);
    dt.setDate(dt.getDate() + diff);
    dt.setHours(0, 0, 0, 0);
    return dt;
}

function addDays(d, days) {
    const r = new Date(d);
    r.setDate(r.getDate() + days);
    r.setHours(0, 0, 0, 0);
    return r;
}

function getISOWeekNumber(d) {
    const date = new Date(Date.UTC(d.getFullYear(), d.getMonth(), d.getDate()));
    date.setUTCDate(date.getUTCDate() + 4 - (date.getUTCDay() || 7));
    const yearStart = new Date(Date.UTC(date.getUTCFullYear(), 0, 1));
    const weekNo = Math.ceil((((date - yearStart) / 86400000) + 1) / 7);
    return weekNo;
}

function getCategoryColor(cat) {
    return localStorage.getItem(`color:${cat}`) || "#4caf50";
}

// --- network ---
async function loadData() {
    const res = await fetch(`/api/resagerhelper/get?user=${userId}`);
    const data = await res.json();
    categories = data.categories || [];
    marks = data.marks || [];
    render();
}

async function toggleDay(cat, dateStr) {
    await fetch(`/api/resagerhelper/toggle?user=${userId}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ category: cat, date: dateStr })
    });
    await loadData();
}

async function syncCategories() {
    await fetch(`/api/resagerhelper/set_categories?user=${userId}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ categories })
    });
}

// --- render ---
function render() {
    const container = document.getElementById("categories");
    container.innerHTML = "";

    for (let cat of categories) {
        let menuId = "menu-" + generateRandomString(5); //
        const div = document.createElement("div");
        div.className = "category";
        div.style.setProperty("--cell-color", getCategoryColor(cat));

        // заголовок
        const nameDiv = document.createElement("div");
        nameDiv.className = "category-name";

        const titleSpan = document.createElement("span");
        titleSpan.textContent = cat;

        // const menuBtn = document.createElement("span");
        // menuBtn.className = "menu-btn";
        // menuBtn.textContent = "⋮"+menuId;//
        // menuBtn.setAttribute("menu-id", menuId);//


        const menu = document.createElement("div");
        menu.className = "category-buttons";
        menu.id = menuId;

        let delBtn = document.createElement("button");
        delBtn.className = "del-btn";
        //delBtn.classList.add("del-btn");
        delBtn.textContent = "🗑";
        //delBtn.style.padding = "8px 10px";
        menu.appendChild(delBtn);

        let settingsBtn = document.createElement("button");
        settingsBtn.className = "settings-btn";
        //settingsBtn.classList.add("settings-btn");
        settingsBtn.textContent = "⚙️";
        //settingsBtn.style.padding = "8px 10px";
        menu.appendChild(settingsBtn);

        nameDiv.appendChild(titleSpan);
        nameDiv.appendChild(menu);
        div.appendChild(nameDiv);
        //div.appendChild(menu);

        // menuBtn.onclick = (e) => {
        //     e.stopPropagation();
        //     document.querySelectorAll(".menu-popup").forEach(m => m.style.display = m.style.display === "block" ? "none" : "block");//
        //     let foundedMenu = document.getElementById(e.target.getAttribute("menu-id"));
        //     console.log(e.target, e.target.getAttribute("menu-id"), foundedMenu, "=====1", foundedMenu.style.display, foundedMenu.style.display === "none");
        //     //foundedMenu.style.display = foundedMenu.style.display === "none" ? "block" : "none";
        //     foundedMenu.style.display = "block";
        //     console.log(e.target, e.target.getAttribute("menu-id"), foundedMenu, "=====2", foundedMenu.style.display, foundedMenu.style.display === "none");
        //     const p = document.createElement("p"); p.textContent = 'qq1'+e.target.getAttribute("menu-id")+"__"+foundedMenu.id; document.body.appendChild(p);
        //     //menu.style.display = menu.style.display === "absolute" ? "none" : "absolute";
        // };
        //
        // document.body.addEventListener("click", () => (menu.style.display = "none"), { once: true });

        delBtn.onclick = async () => {
            categories = categories.filter(c => c !== cat);
            await syncCategories();
            await loadData();
        };

        settingsBtn.onclick = () => {
            pageMain.style.display = "none";
            pageSettingsCategory.style.display = "block";

            settingsCategoryNameInput.value = cat;

            const current = getCategoryColor(cat);
            settingsCategoryColorInput.value = current;
            settingsCategoryColorInput.setAttribute("category-name", cat);//


            // const input = document.createElement("input");
            // const p = document.createElement("p"); p.textContent = '__'+current+"__"+foundedMenu.id; document.body.appendChild(p);
            // input.type = "color";
            // input.value = current;
            // input.style.position = "absolute";
            // input.style.left = "-9999px";
            // document.body.appendChild(input);
            // input.click();
            // input.oninput = (e) => {
            //     localStorage.setItem(`color:${cat}`, e.target.value);
            //     render();
            // };
            // input.onchange = () => document.body.removeChild(input);
        };

        // основное тело категории: сетка + дни
        const body = document.createElement("div");
        body.className = "category-body";

        const weeksContainer = document.createElement("div");
        weeksContainer.className = "weeks";

        const currentWeekStart = getWeekStart(today);
        const oldest = new Date(currentWeekStart);
        oldest.setDate(oldest.getDate() - (WEEKS_TO_SHOW - 1) * DAYS_PER_WEEK);

        for (let w = 0; w < WEEKS_TO_SHOW; w++) {
            const weekStart = addDays(oldest, w * DAYS_PER_WEEK);
            const weekNo = getISOWeekNumber(weekStart);
            const labelText = `W${weekNo}\n${String(weekStart.getDate()).padStart(2, "0")}.${String(weekStart.getMonth() + 1).padStart(2, "0")}`;

            const weekEl = document.createElement("div");
            weekEl.className = "week";

            const label = document.createElement("div");
            label.className = "week-label";
            label.textContent = labelText;
            weekEl.appendChild(label);

            const cells = document.createElement("div");
            cells.className = "week-cells";

            for (let d = 0; d < DAYS_PER_WEEK; d++) {
                const day = addDays(weekStart, d);
                const dateStr = day.toISOString().split("T")[0];

                const cell = document.createElement("div");
                cell.className = "cell";
                cell.setAttribute("data-date", formatDate(day));
                if (marks.find(m => m.date === dateStr && m.category === cat)) {
                    cell.classList.add("active");
                }
                cell.onclick = () => toggleDay(cat, dateStr);
                cells.appendChild(cell);
            }

            weekEl.appendChild(cells);
            weeksContainer.appendChild(weekEl);
        }

        const dayLabels = document.createElement("div");
        dayLabels.className = "day-labels";
        for (let n of dayNames) {
            const span = document.createElement("div");
            span.textContent = n;
            dayLabels.appendChild(span);
        }

        body.appendChild(weeksContainer);
        //body.appendChild(dayLabels);
        weeksContainer.appendChild(dayLabels);
        div.appendChild(body);
        container.appendChild(div);
    }
}

// добавление категории
document.getElementById("add-btn").onclick = async () => {
    const input = document.getElementById("new-category");
    const val = input.value.trim();
    if (!val || categories.includes(val)) return;
    categories.push(val);
    input.value = "";
    await syncCategories();
    await loadData();
};

loadData();

// переключение вкладок
document.querySelectorAll(".tab-btn").forEach(btn => {
    btn.onclick = () => {
        document.querySelectorAll(".tab-btn").forEach(b => b.classList.remove("active"));
        document.querySelectorAll(".tab").forEach(tab => tab.classList.remove("active"));
        btn.classList.add("active");
        document.getElementById(`tab-${btn.dataset.tab}`).classList.add("active");
    };
});

// --- Checks ---
let checkGroups = JSON.parse(localStorage.getItem("checkGroups") || "[]"); //

function saveChecks() {
    localStorage.setItem("checkGroups", JSON.stringify(checkGroups));
}

function renderChecks() {
    const container = document.getElementById("check-groups");
    container.innerHTML = "";
    for (let group of checkGroups) {
        const div = document.createElement("div");
        div.className = "check-group";

        const title = document.createElement("h3");
        title.textContent = group.name;
        div.appendChild(title);

        const addInput = document.createElement("input");
        addInput.placeholder = "Добавить пункт...";
        addInput.className = "add-group-input";
        const addBtn = document.createElement("button");
        addBtn.textContent = "➕";
        addBtn.className = "add-group-btn";
        addBtn.onclick = () => {
            const text = addInput.value.trim();
            if (!text) return;
            group.items.push({ text, done: false });
            addInput.value = "";
            saveChecks();
            renderChecks();
        };
        div.appendChild(addInput);
        div.appendChild(addBtn);

        for (let item of group.items) {
            const row = document.createElement("div");
            row.className = "check-item";
            const checkbox = document.createElement("input");
            checkbox.type = "checkbox";
            checkbox.checked = item.done;
            checkbox.onchange = () => {
                item.done = checkbox.checked;
                saveChecks();
            };
            const label = document.createElement("span");
            label.textContent = item.text;
            row.appendChild(checkbox);
            row.appendChild(label);
            div.appendChild(row);
        }

        container.appendChild(div);
    }
}

document.getElementById("add-group-btn").onclick = () => {
    const input = document.getElementById("new-group");
    const val = input.value.trim();
    if (!val || checkGroups.some(g => g.name === val)) return;
    checkGroups.push({ name: val, items: [] });
    input.value = "";
    saveChecks();
    renderChecks();
};

renderChecks();