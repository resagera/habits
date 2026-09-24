const tg = window.Telegram.WebApp;
const user = "testuser";
const apiBase = "/api/resagerhelper";
const apiBaseV3 = "http://localhost:8676/api/habits";
const daysShort = ["Пн","Вт","Ср","Чт","Пт","Сб","Вс"];
const userId = tg.initDataUnsafe?.user?.id || "testuser";
let linkStoreLocally = localStorage.getItem("linkStoreLocally") !== "false";
document.getElementById("link-store-locally-toggle").onchange = e => {
    linkStoreLocally = e.target.checked;
    localStorage.setItem("linkStoreLocally", linkStoreLocally);
};
if (linkStoreLocally) {
    document.getElementById("link-store-locally-toggle").checked = true;
    document.getElementById("store-in-local-link-notice").style.display = "block";
}

const WEEKS_TO_SHOW = 8;
const DAYS_PER_WEEK = 7;

let categories = [];
const today = new Date();
let marks = [];

const dayNames = ["Пн", "Вт", "Ср", "Чт", "Пт", "Сб", "Вс"];
let editingCategory = null;

const trackerTab = document.getElementById("trackerTab");
const checksTab = document.getElementById("checksTab");
const diaryTab = document.getElementById("diaryTab");
const metricsTab = document.getElementById("metricsTab");
const passTab = document.getElementById("passTab");
const exchangeTab = document.getElementById("exchangeTab");
const linksTab = document.getElementById("tab-links");

const tabTrackerBtn = document.getElementById("tab-tracker");
const tabChecksBtn = document.getElementById("tab-checks");
const tabDiaryBtn = document.getElementById("tab-diary");
const tabMetricsBtn = document.getElementById("tab-metrics");
const passBtn = document.getElementById("tab-pass");
const exchangeBtn = document.getElementById("tab-exchange");
const tabLinksBtn = document.getElementById("tab-links-btn");

let tabButtons = [tabTrackerBtn, tabChecksBtn, tabMetricsBtn, tabDiaryBtn, passBtn, exchangeBtn, tabLinksBtn]
let pages = [trackerTab, checksTab, metricsTab, diaryTab, passTab, exchangeTab, linksTab]

function hideAllPages() {
    pages.forEach(p => {if (p) { p.classList.add("hidden"); }});
}

function unselectButtons() {
    tabButtons.forEach(b => {if (b) {b.classList.remove("border-blue-500", "font-semibold");}});
}

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
function tabTrigger(e) {
    e.stopPropagation();
    sidebar.classList.remove('active');
    overlay.classList.remove('active');
    let el = e.target;
    for (let i = 0; i < 20; i++) {
        console.log(el, el.tagName, el.tagName.toLowerCase() !== "button");
        if (el.tagName.toLowerCase() !== "button") {
            el = el.parentNode;
        } else {
            break;
        }
    }
    console.log(el, el.tagName, el.dataset, el.dataset.tab);
    localStorage.setItem("last-active-tab", el.dataset.tab);
    localStorage.setItem("last-active-tab-title", el.dataset.title);
    pages.forEach(p => {if (p) { p.classList.remove("active"); }});
    unselectButtons();
    el.classList.add("border-blue-500", "font-semibold");
    document.getElementById(el.dataset.tab).classList.add("active");
    appTitle.textContent = el.dataset.title;
}

tabTrackerBtn.onclick = (e) => tabTrigger(e);
tabChecksBtn.onclick = (e) => tabTrigger(e);
tabDiaryBtn.onclick = (e) => tabTrigger(e);
tabMetricsBtn.onclick = (e) => tabTrigger(e);
passBtn.onclick = (e) => tabTrigger(e);
exchangeBtn.onclick = (e) => tabTrigger(e);
tabLinksBtn.onclick = (e) => tabTrigger(e);

const pageMain = document.getElementById("page-main");
const pageSettingsCategory = document.getElementById("page-category-settings");
const pageSettingsGroup = document.getElementById("page-group-settings");
const pageSettingsGlobal = document.getElementById("page-global-settings");

const appTitle = document.getElementById("main-title");

const menuToggle = document.getElementById('main-menu-button');
const sidebar = document.getElementById('sidebar');
const overlay = document.getElementById('overlay');

menuToggle.addEventListener('click', () => {
    sidebar.classList.add('active');
    overlay.classList.add('active');
});

overlay.addEventListener('click', () => {
    sidebar.classList.remove('active');
    overlay.classList.remove('active');
});

let currentCategory = null;
let currentMonth = new Date();
const calendarModal = document.getElementById('calendar-modal');
const calendarContainer = document.getElementById('calendar-container');

const saveBtn = document.getElementById("save");
const saveGlobalBtn = document.getElementById("save-global-settings");

saveBtn.onclick = async (e) => {
    pageSettingsCategory.style.display = "none";
    pageSettingsGroup.style.display = "none";
    pageSettingsGlobal.style.display = "none";
    pageMain.style.display = "block";
};
saveGlobalBtn.onclick = async (e) => {
    saveSettings();
};

function saveSettings() {
    pageSettingsCategory.style.display = "none";
    pageSettingsGroup.style.display = "none";
    pageSettingsGlobal.style.display = "none";
    pageMain.style.display = "block";
}


const globalSettingsTabBtn = document.getElementById("tab-settings");

globalSettingsTabBtn.onclick = () => {
    pageMain.style.display = "none";
    pageSettingsGlobal.style.display = "block";
    sidebar.classList.remove('active');
    overlay.classList.remove('active');
};

document.addEventListener('DOMContentLoaded', (event) => {
    const now = new Date();

    const year = now.getFullYear();
    const month = (now.getMonth() + 1).toString().padStart(2, '0');
    const day = now.getDate().toString().padStart(2, '0');
    const hours = now.getHours().toString().padStart(2, '0');
    const minutes = now.getMinutes().toString().padStart(2, '0');

    const formattedDateTime = `${year}-${month}-${day}T${hours}:${minutes}`;

    document.getElementById('diary-date').value = formattedDateTime;
});

// --- settings ---
const settingsCategoryNameInput = document.getElementById("category-name");
const settingsCategoryNamePreviewInput = document.getElementById("category-name-preview");
const settingsCategoryColorInput = document.getElementById("category-color");

settingsCategoryColorInput.onchange = async (e) => { //oninput onchange
    const cat = e.target.getAttribute("category-name");

    const editingCategory = document.getElementById("category-name-preview").value;
    const color = document.getElementById("category-color").value;
    await fetch(`${apiBase}/set_color?user=${userId}`, {
        method: "POST",
        headers: {"Content-Type": "application/json"},
        body: JSON.stringify({category: editingCategory, color}),
    });
    await loadData();
};

async function saveCategories() {
    await fetch(`${apiBase}/set_categories?user=${userId}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ categories: categories.map(c => c.name) }),
    });
}

// --- helpers ---
function formatDate(d) {
    const dd = String(d.getDate()).padStart(2, "0");
    const mm = String(d.getMonth() + 1).padStart(2, "0");
    const yyyy = d.getFullYear();
    return `${dd}.${mm}.${yyyy}`;
}

function formatDateForAPI(d) {
    const year = d.getFullYear();
    const month = String(d.getMonth() + 1).padStart(2, '0');
    const day = String(d.getDate()).padStart(2, '0');
    return `${year}-${month}-${day}`;
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
    const foundCategory = categories.find(category => category.name === cat);
    return foundCategory.color || "#22c55e";
    //return localStorage.getItem(`color:${cat.color}`) || "#22c55e"; //4caf50
}

// --- network ---
async function loadData() {
    const res = await fetch(`${apiBase}/get?user=${userId}`);
    //const res = await fetch(`/api/resagerhelper/get?user=${userId}`);
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

function getLast10Weeks() {
    const today = new Date();
    const result = [];
    for (let w = 0; w < 10; w++) {
        const week = [];
        for (let d = 0; d < 7; d++) {
            const date = new Date(today);
            date.setDate(today.getDate() - (w * 7 + (6 - d)));
            week.push(date.toISOString().slice(0, 10));
        }
        result.push(week);
    }
    return result.reverse();
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
    const weeks = getLast10Weeks();

    //for (let cat of categories) {
    categories.forEach(cat => {
        const catMarks = marks.filter(m => m.category === cat.name).map(m => m.date);
        const color = cat.color || "#22c55e";
        let menuId = "menu-" + generateRandomString(5); //
        const div = document.createElement("div");
        div.className = "category";
        div.style.setProperty("--cell-color", color);

        // заголовок
        const nameDiv = document.createElement("div");
        nameDiv.className = "category-name";

        const titleSpan = document.createElement("span");
        titleSpan.textContent = cat.name;

        const menu = document.createElement("div");
        menu.className = "category-buttons";
        menu.id = menuId;

        let calendarBtn = document.createElement("button");
        calendarBtn.className = "settings-btn bg-gray-200 hover:bg-gray-300 px-2 py-1 rounded";
        calendarBtn.textContent = "📅";
        calendarBtn.id ="open-calendar";
        menu.appendChild(calendarBtn);

        calendarBtn.onclick = async () => {
            console.log("open calendar");
            await openCalendar(cat.name);
        };

        let delBtn = document.createElement("button");
        delBtn.className = "del-btn";
        delBtn.textContent = "🗑";
        menu.appendChild(delBtn);

        let settingsBtn = document.createElement("button");
        settingsBtn.className = "settings-btn";
        settingsBtn.textContent = "⚙️";
        menu.appendChild(settingsBtn);

        nameDiv.appendChild(titleSpan);
        nameDiv.appendChild(menu);
        div.appendChild(nameDiv);

        delBtn.onclick = async () => {
            categories = categories.filter(c => c.name !== cat.name);
            await saveCategories(); //syncCategories
            await loadData();
        };

        settingsBtn.onclick = () => {
            pageMain.style.display = "none";
            pageSettingsCategory.style.display = "block";

            settingsCategoryNamePreviewInput.value = cat.name;
            settingsCategoryNameInput.value = cat.name;

            const current = getCategoryColor(cat.name);
            settingsCategoryColorInput.value = current;
            settingsCategoryColorInput.setAttribute("category-name", cat);//


        };

        // основное тело категории: сетка + дни
        const body = document.createElement("div");
        body.className = "category-body";

        const weeksContainer = document.createElement("div");
        weeksContainer.className = "weeks";

        const currentWeekStart = getWeekStart(today);
        const oldest = new Date(currentWeekStart);
        oldest.setDate(oldest.getDate() - (WEEKS_TO_SHOW - 1) * DAYS_PER_WEEK);
        const currentDate = formatDate(new Date())
        let formattedDate = "";
        for (let w = 0; w < WEEKS_TO_SHOW; w++) {
            const weekStart = addDays(oldest, w * DAYS_PER_WEEK);
            //const weekNo = getISOWeekNumber(weekStart);
            //const labelText = `W${weekNo}\n${String(weekStart.getDate()).padStart(2, "0")}.${String(weekStart.getMonth() + 1).padStart(2, "0")}`;
            const labelText = `${String(weekStart.getDate()).padStart(2, "0")}.${String(weekStart.getMonth() + 1).padStart(2, "0")}`;

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
                const dateStr = formatDateForAPI(day);

                const cell = document.createElement("div");
                cell.className = "cell";
                formattedDate = formatDate(day);
                if (formattedDate === currentDate) {
                    cell.className = "cell current-date";
                }
                cell.setAttribute("data-date", formattedDate);
                if (marks.find(m => m.date === dateStr && m.category === cat.name)) {
                    cell.classList.add("active");
                }
                cell.onclick = () => toggleDay(cat.name, dateStr);
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
    });
}

// добавление категории
document.getElementById("add-btn").onclick = async () => {
    const input = document.getElementById("new-category");
    console.log(input.value);
    const name = input.value.trim();
    if (!name || categories.includes(name)) return;
    //categories.push(name);
    categories.push({ name, color: "#22c55e" });
    input.value = "";
    console.log(categories);
    await saveCategories();
    console.log(categories);
    await loadData();
    console.log(categories);
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

async function loadChecks() {
    const res = await fetch(`${apiBase}/get_checks?user=${userId}`);
    const data = await res.json();
    if (data) {
        renderChecks(data);
    }
}

function saveChecks() {
    localStorage.setItem("checkGroups", JSON.stringify(checkGroups));
}

function renderChecks(groups) {
    const container = document.getElementById("checkGroups");
    container.innerHTML = "";

    for (const group of groups) {
        const groupEl = document.createElement("div");
        groupEl.className = "p-1 rounded shadow relative";

        const title = document.createElement("div");
        title.className = "flex justify-between items-center mb-2";
        title.innerHTML = `
      <span class="font-semibold">${group.name}</span>
      <button class="menu-btn text-gray-600 hover:text-black">☰</button>
    `;

        // Меню группы
        const menu = document.createElement("div");
        menu.className = "absolute right-2 top-8 border rounded shadow-md hidden";
        menu.innerHTML = `
      <button class="block w-full text-left px-4 py-2 hover:bg-gray-100 text-sm edit-group">✏️ Переименовать</button>
      <button class="block w-full text-left px-4 py-2 hover:bg-gray-100 text-sm add-item">➕ Добавить пункт</button>
      <button class="block w-full text-left px-4 py-2 hover:bg-gray-100 text-sm text-red-600 delete-group">🗑 Удалить группу</button>
    `;

        // Обработчик открытия меню
        title.querySelector(".menu-btn").onclick = (e) => {
            e.stopPropagation();
            document.querySelectorAll(".group-menu").forEach(m => m.classList.add("hidden"));
            menu.classList.toggle("hidden");
        };
        menu.classList.add("group-menu");

        document.addEventListener("click", () => menu.classList.add("hidden"));

        // Обработчики меню
        menu.querySelector(".edit-group").onclick = async () => {
            const newName = prompt("Новое название группы:", group.name);
            if (!newName || newName === group.name) return;
            await fetch(`${apiBase}/rename_check_group?user=${userId}`, {
                method: "POST",
                headers: {"Content-Type": "application/json"},
                body: JSON.stringify({old: group.name, new: newName}),
            });
            await loadChecks();
            showToast("Название изменено ✏️");
        };

        menu.querySelector(".add-item").onclick = async () => {
            const itemName = prompt("Введите название пункта:");
            if (!itemName) return;
            await fetch(`${apiBase}/add_check_item?user=${userId}`, {
                method: "POST",
                headers: {"Content-Type": "application/json"},
                body: JSON.stringify({group: group.name, name: itemName}),
            });
            await loadChecks();
            showToast("Пункт добавлен ✅");
        };

        menu.querySelector(".delete-group").onclick = async () => {
            if (!confirm(`Удалить группу "${group.name}"?`)) return;
            await fetch(`${apiBase}/delete_check_group?user=${userId}`, {
                method: "POST",
                headers: {"Content-Type": "application/json"},
                body: JSON.stringify({name: group.name}),
            });
            await loadChecks();
            showToast("Группа удалена 🗑");
        };

        const checks = document.createElement("div");
        const items = Array.isArray(group.items) ? group.items : [];

        for (const check of items) {
            const item = document.createElement("div");
            item.className = "flex items-center justify-between mb-1 check-item";

            const code = generateRandomString(10);
            item.innerHTML = `
          <input class="inp-cbx" id="${code}" type="checkbox" style="display: none;" ${check.done ? "checked" : ""} data-group="${group.name}" data-item="${check.name}"/>
          <label class="cbx" for="${code}">
            <span>
              <svg width="12px" height="9px" viewbox="0 0 12 9">
                <polyline points="1 5 4 8 11 1"></polyline>
              </svg>
            </span>
            <span>${check.name}</span>
          </label>
        <button class="text-gray-500 hover:text-red-600 delete-item">✖</button>
      `;

            const checkbox = item.querySelector("input");
            checkbox.onchange = async (e) => {
                await fetch(`${apiBase}/toggle_check?user=${userId}`, {
                    method: "POST",
                    headers: {"Content-Type": "application/json"},
                    body: JSON.stringify({
                        group: group.name,
                        item: check.name,
                        done: e.target.checked
                    }),
                });
                showToast(e.target.checked ? "✅ Отмечено" : "❌ Снято");
            };

            item.querySelector(".delete-item").onclick = async () => {
                if (!confirm(`Удалить пункт "${check.name}"?`)) return;
                await fetch(`${apiBase}/delete_check_item?user=${userId}`, {
                    method: "POST",
                    headers: {"Content-Type": "application/json"},
                    body: JSON.stringify({group: group.name, name: check.name}),
                });
                await loadChecks();
                showToast("Пункт удалён 🗑");
            };

            checks.appendChild(item);
        }

        groupEl.appendChild(title);
        groupEl.appendChild(menu);
        groupEl.appendChild(checks);
        container.appendChild(groupEl);
    }
}

function renderChecks_() {
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

document.getElementById("addCheckGroup").onclick = async () => {
    const name = prompt("Введите название группы:");
    if (!name) {
        showToast("Введите имя группы");
        return;
    }
    if (checkGroups.some(g => g.name === name)) {
        showToast("Такая группа уже есть");
        return;
    }
    checkGroups.push({ name: name, items: [] });
    await fetch(`${apiBase}/add_check_group?user=${userId}`, {
        method: "POST",
        headers: {"Content-Type": "application/json"},
        body: JSON.stringify({name}),
    });
    await loadChecks();
    saveChecks();
    showToast("Группа добавлена ✅");
};

//renderChecks();
loadChecks();

function showToast(message) {
    const toast = document.getElementById("toast");
    toast.textContent = message;
    toast.classList.remove("opacity-0");
    toast.classList.add("opacity-100");

    setTimeout(() => {
        toast.classList.remove("opacity-100");
        toast.classList.add("opacity-0");
    }, 2000);
}

const diaryDate = document.getElementById('diary-date');
const diaryText = document.getElementById('diary-text');
const diarySave = document.getElementById('diary-save');
const diaryList = document.getElementById('diary-list');
const diarySearch = document.getElementById('diary-search');

async function loadDiary() {
    const from = document.getElementById('filter-from').value;
    const to = document.getElementById('filter-to').value;
    const q = document.getElementById('diary-search').value.trim();

    let url = `/api/resagerhelper/diary?user=${userId}`;
    if (from) url += `&from=${from}`;
    if (to) url += `&to=${to}`;

    // если выполняется поиск — используем /search
    if (q) {
        url = `/api/resagerhelper/diary/search?user=${userId}&q=${encodeURIComponent(q)}`;
        if (from) url += `&from=${from}`;
        if (to) url += `&to=${to}`;
    }

    const res = await fetch(url);
    const entries = await res.json();
    renderDiary(entries);
}

document.getElementById('filter-apply').onclick = loadDiary;
//diarySearch.oninput = debounce(loadDiary, 300); // можно добавить debounce


let editingId = null;

function renderDiary(entries) {
    const diaryList = document.getElementById('diary-list');
    diaryList.innerHTML = '';

    if (!entries) {return;}

    entries.forEach(e => {
        const wrapper = document.createElement('div');
        wrapper.className = 'border p-2 rounded relative group';

        // Контейнер для кнопок (показывается при наведении)
        const controls = document.createElement('div');
        controls.className = 'absolute right-2 top-2 flex space-x-2';

        // Кнопка редактировать
        const editBtn = document.createElement('button');
        editBtn.type = 'button';
        editBtn.className = 'text-blue-500 hover:text-blue-700';
        editBtn.title = 'Редактировать';
        editBtn.textContent = '✏️';
        editBtn.addEventListener('click', () => editDiary(e.id, e.date, e.text));

        // Кнопка удалить
        const delBtn = document.createElement('button');
        delBtn.type = 'button';
        delBtn.className = 'text-red-500 hover:text-red-700';
        delBtn.title = 'Удалить';
        delBtn.textContent = '🗑️';
        delBtn.addEventListener('click', () => deleteDiary(e.id));

        controls.appendChild(editBtn);
        controls.appendChild(delBtn);

        // Дата
        const dateDiv = document.createElement('div');
        dateDiv.className = 'text-sm text-gray-500';
        dateDiv.textContent = e.date;

        // Текст записи — используем textContent и pre-wrap, чтобы сохранить переносы
        const textDiv = document.createElement('div');
        textDiv.style.whiteSpace = 'pre-wrap';
        textDiv.textContent = e.text;

        wrapper.appendChild(controls);
        wrapper.appendChild(dateDiv);
        wrapper.appendChild(textDiv);

        diaryList.appendChild(wrapper);
    });
}


// Функция редактирования (подставляет в поля и переключает кнопку)
function editDiary(id, date, text) {
    editingId = id;
    const diaryDate = document.getElementById('diary-date');
    const diaryText = document.getElementById('diary-text');
    const diarySave = document.getElementById('diary-save');

    diaryDate.value = date;
    diaryText.value = text;
    diarySave.textContent = 'Обновить';
    // можно прокрутить к форме, если нужно:
    // diaryDate.scrollIntoView({behavior: 'smooth', block: 'center'});
}

// Функция удаления — та же, но оставляю здесь наглядно
async function deleteDiary(id) {
    if (!confirm('Удалить эту запись?')) return;
    await fetch(`/api/resagerhelper/diary/${id}`, { method: 'DELETE' });
    showToast('Запись удалена 🗑️');
    // Перезагрузить список
    await loadDiary();
}
diarySearch.oninput = async () => {
    const q = diarySearch.value.trim();
    if (q.length === 0) return loadDiary();

    const res = await fetch(`/api/resagerhelper/diary/search?user=${userId}&q=${encodeURIComponent(q)}`);
    const entries = await res.json();
    renderDiary(entries);
};

diarySave.onclick = async () => {
    const text = diaryText.value.trim();
    if (!text) return showToast('Введите текст записи');

    if (editingId) {
        await fetch(`/api/resagerhelper/diary/${editingId}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ date: diaryDate.value, text }),
        });
        showToast("Запись обновлена ✅");
        editingId = null;
        diarySave.textContent = "Сохранить";
    } else {
        await fetch(`/api/resagerhelper/diary?user=${userId}`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ date: diaryDate.value, text }),
        });
        showToast("Запись добавлена ✅");
    }

    diaryText.value = '';
    loadDiary();
};

loadDiary();

document.getElementById('export-txt').addEventListener('click', () => exportDiary('txt'));
document.getElementById('export-csv').addEventListener('click', () => exportDiary('csv'));
const bgList = document.getElementById('bg-preset');
const currentBg = document.getElementById('bg-url');

async function exportDiary(type) {
    const from = document.getElementById('export-from').value;
    const to = document.getElementById('export-to').value;

    if (!from || !to) {
        showToast('Выберите период 📅');
        return;
    }

    const res = await fetch(`/api/resagerhelper/diary/export?from=${from}&to=${to}&type=${type}&user=${userId}`);
    if (!res.ok) {
        showToast('Ошибка при экспорте ❌');
        return;
    }

    const blob = await res.blob();
    const url = window.URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `diary_${from}_${to}.${type}`;
    a.click();
    window.URL.revokeObjectURL(url);
    showToast('Файл выгружен ✅');
}
document.getElementById('bg-save').addEventListener('click', async () => {
    await saveBackground();
});
document.getElementById('bg-reset').addEventListener('click', async () => {
    const formData = new FormData();
    formData.append('id', 0);
    const res = await fetch(`/api/resagerhelper/settings/background?user=${userId}`, {
        method: 'POST',
        body: formData
    });
    await loadBackground();
});

async function saveBackground() {
    const position = document.getElementById('bg-position').value;
    const fileInput = document.getElementById('bg-upload');
    const file = fileInput.files[0];

    const formData = new FormData();
    formData.append('position', position);
    if (bgList.value) formData.append('id', bgList.value);
    if (file) formData.append('file', file);

    const res = await fetch(`/api/resagerhelper/settings/background?user=${userId}`, {
        method: 'POST',
        body: formData
    });

    if (res.ok) {
        showToast('Фон сохранён ✅');
        await loadBackground(); // применим сразу
    } else {
        showToast('Ошибка сохранения фона ❌');
    }
}


async function loadBackground() {
    const res = await fetch(`/api/resagerhelper/settings/background?user=${userId}`);
    if (!res.ok) return;
    const data = await res.json();

    currentBg.value = data.url ? data.url : '';
    let bgLayer = document.getElementById('fixed-background');
    bgLayer.style.backgroundImage = data.url ? `url(${data.url})` : '';
    bgLayer.style.backgroundSize = data.position === 'cover' ? 'cover' : 'auto';
    bgLayer.style.backgroundRepeat = data.position === 'repeat' ? 'repeat' : 'no-repeat';
    bgLayer.style.backgroundPosition = data.position === 'center' ? 'center' : 'top left';
    bgLayer.style.backgroundAttachment = 'fixed';
    if (!data.urls) {
        return;
    }
    document.querySelectorAll('#bg-preset option').forEach(option => {if(option.value){option.remove()}});
    Object.entries(data.urls).forEach(([value, text]) => {
        const option = document.createElement('option');
        option.value = value;
        option.textContent = text; // или можно показать только имя файла, см. ниже
        bgList.appendChild(option);
    });
}
loadBackground();

document.getElementById('theme-save').addEventListener('click', async () => {
    const theme = document.querySelector('input[name="theme"]:checked').value;

    const res = await fetch(`/api/resagerhelper/settings/theme?user=${userId}`, {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({theme})
    });

    if (res.ok) {
        applyTheme(theme);
        showToast('Тема сохранена ✅');
    } else {
        showToast('Ошибка при сохранении темы ❌');
    }
});

async function loadTheme() {
    const res = await fetch(`/api/resagerhelper/settings/theme?user=${userId}`);
    let theme = 'light';
    if (res.ok) {
        const data = await res.json();
        if (data.theme) {
            theme = data.theme;
        } else {
            // 🧠 Нет темы в БД → определяем автоматически
            if (window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches) {
                theme = 'dark';
            } else {
                theme = 'light';
            }
            await saveThemeAuto(theme); // сохранить на сервере
        }
    }
    applyTheme(theme);
    const radio = document.querySelector(`input[name="theme"][value="${theme}"]`);
    if (radio) radio.checked = true;
}

async function saveThemeAuto(theme) {
    await fetch(`/api/resagerhelper/settings/theme?user=${userId}`, {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({theme})
    });
}

function applyTheme(theme) {
    document.body.dataset.theme = theme;
}
loadTheme();

// window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', e => {
//     const newTheme = e.matches ? 'dark' : 'light';
//     applyTheme(newTheme);
//     saveThemeAuto(newTheme);
// });
async function openCalendar(category) {
    console.log("open calendar a");
    currentCategory = category;
    currentMonth = new Date(); // стартуем с текущего месяца
    await renderCalendar();
    calendarModal.style.display = 'block';
    calendarModal.classList.remove("hidden");
}

async function renderCalendar() {
    //const user = tg.initDataUnsafe?.user?.id || 'test'; // или свой ID
    const year = currentMonth.getFullYear();
    const month = currentMonth.getMonth();
    const monthStr = `${year}-${String(month+1).padStart(2,'0')}`;

    // получаем отметки
    const res = await fetch(`/api/resagerhelper/marks?user=${userId}&category=${currentCategory}&month=${monthStr}`);
    const data = await res.json();
    const markedDays = new Set(data.days || []);

    // отрисовка календаря
    let html = '<div class="calendar-weekdays"> <div>ПН</div><div>ВТ</div><div>СР</div> <div>ЧТ</div><div>ПТ</div><div>СБ</div><div>ВС</div> </div> <div id="calendar-container"></div><div class="calendar-grid">';
    const end = new Date(year, month+1, 0);

    const date = new Date(year, month, 1);
    const firstDay = (date.getDay() + 6) % 7; // делаем понедельник = 0, воскресенье = 6
    // добавляем пустые блоки до начала месяца
    for (let i = 0; i < firstDay; i++) {
        html += `<div class="calendar-day empty"></div>`;
    }
    for (let d = 1; d <= end.getDate(); d++) {
        const marked = markedDays.has(d);
        const dateStr = `${monthStr}-${String(d).padStart(2,'0')}`;
        html += `<div class="calendar-day ${marked ? 'marked' : ''}" data-date="${dateStr}">${d}</div>`;
    }
    html += '</div>';
    calendarContainer.innerHTML = html;

    // клик по дню — toggle mark
    document.querySelectorAll('.calendar-day').forEach(day => {
        day.addEventListener('click', async (e) => {
            const el = e.target;
            const date = el.dataset.date;
            const body = { category: currentCategory, date };
            await fetch(`/api/resagerhelper/toggle?user=${userId}`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(body)
            });
            el.classList.toggle('marked');
        });
    });
}

// Навигация по месяцам
document.getElementById('prev-month').onclick = async () => {
    currentMonth.setMonth(currentMonth.getMonth() - 1);
    await renderCalendar();
};
document.getElementById('next-month').onclick = async () => {
    currentMonth.setMonth(currentMonth.getMonth() + 1);
    await renderCalendar();
};

// Закрытие модалки
calendarModal.querySelector('.close').onclick = () => {
    calendarModal.style.display = 'none';
    calendarModal.classList.add("hidden");
    loadData();
};

document.getElementById("export-checks").addEventListener("click", async () => {
    try {
        const res = await fetch(`/api/resagerhelper/export-checks?user=${userId}`);
        if (!res.ok) throw new Error("Ошибка экспорта");

        const blob = await res.blob();
        const url = window.URL.createObjectURL(blob);

        const a = document.createElement("a");
        a.href = url;
        a.download = `checks_${userId}_${new Date().toISOString().slice(0,10)}.csv`;
        a.click();

        window.URL.revokeObjectURL(url);
        showToast('Файл сохранен ✅');
    } catch (err) {
        showToast('Ошибка при экспорте ' + err.message);
    }
});

// ==== CONFIG ====
const API_BASE = "/api/resagerhelper";
//const USER_ID = window.Telegram?.WebApp?.initDataUnsafe?.user?.id || "demo_user";

// ==== STATE ====
let metrics = []; // list of metrics from server
let selectedMetricId = null;
let compareMode = false;
let selectedCompare = new Set(); // metric ids to compare
let currentPeriod = "week";
let chart = null;

// ==== UI refs ====
const elMetricsList = document.getElementById("metrics-list");
const elAddMetricBtn = document.getElementById("add-metric-btn");
const elMetricName = document.getElementById("metric-name");
const elMetricMax = document.getElementById("metric-max");
const elMetricColor = document.getElementById("metric-color");
const ctx = document.getElementById("metrics-chart").getContext("2d");
const elCompareCheckbox = document.getElementById("compare-mode");
const periodBtns = document.querySelectorAll(".period-btn");
const elLegend = document.getElementById("chart-legend");

// load list
async function loadMetrics() {
    const res = await fetch(`${API_BASE}/metrics/list?user=${userId}`);
    metrics = await res.json();
    renderMetricsList();
    // if no selected -> choose first
    if (!metrics) {
        return;
    }
    if (!selectedMetricId && metrics.length) {
        selectedMetricId = metrics[0].id;
    }
    await refreshChart();
}

function renderMetricsList() {
    elMetricsList.innerHTML = "";
    if (!metrics) {return}
    for (const m of metrics) {
        const item = document.createElement("div");
        item.className = "metric-item";
        item.innerHTML = `
      <div>
        <div style="font-weight:600">${escapeHtml(m.name)}</div>
        <div class="meta">max/day: ${m.max_per_day===0? '∞' : m.max_per_day} </div>
      </div>
      <div>
        <button data-id="${m.id}" class="select-metric">📈</button>
        <button data-id="${m.id}" class="add-value">➕</button>
        <button data-id="${m.id}" class="delete-metric">🗑</button>
        <label style="margin-left:8px;"><input type="checkbox" class="compare-check" data-id="${m.id}" ${selectedCompare.has(String(m.id)) ? 'checked' : ''}/> сравнить</label>
      </div>
    `;
        elMetricsList.appendChild(item);
    }

    // attach handlers
    elMetricsList.querySelectorAll(".select-metric").forEach(b => b.onclick = async (e) => {
        selectedMetricId = Number(e.target.dataset.id);
        if (!compareMode) selectedCompare.clear();
        await refreshChart();
    });
    elMetricsList.querySelectorAll(".add-value").forEach(b => b.onclick = async (e) => {
        const id = Number(e.target.dataset.id);
        await promptAddValue(id);
    });
    elMetricsList.querySelectorAll(".delete-metric").forEach(b => b.onclick = async (e) => {
        const id = e.target.dataset.id;
        if (!confirm("Удалить параметр и все его значения?")) return;
        await fetch(`${API_BASE}/metrics?id=${id}&user=${userId}`, { method: "DELETE" });
        await loadMetrics();
    });
    elMetricsList.querySelectorAll(".compare-check").forEach(cb => cb.onchange = async (e) => {
        const id = String(e.target.dataset.id);
        if (e.target.checked) selectedCompare.add(id); else selectedCompare.delete(id);
        await refreshChart();
    });
}

// add metric
elAddMetricBtn.onclick = async () => {
    const name = elMetricName.value.trim();
    const max = Number(elMetricMax.value);
    const color = elMetricColor.value || "#22c55e";
    if (!name) { alert("Введите название"); return; }
    const res = await fetch(`${API_BASE}/metrics/create`, {
        method: "POST",
        headers: {"Content-Type":"application/json"},
        body: JSON.stringify({ user: ""+userId, name, max_per_day: max, color })
    });
    if (!res.ok) { alert("Ошибка создания " + res.status + await res.text()); return; }
    elMetricName.value = "";
    await loadMetrics();
};

// prompt add numeric value
async function promptAddValue(metricId) {
    const raw = prompt("Введите значение (число, можно с запятой):");
    if (raw === null) return;
    const normalized = raw.replace(",", ".").trim();
    const val = Number(normalized);
    if (isNaN(val)) { alert("Нужно число"); return; }
    const body = { user: ""+userId, metric_id: metricId, value: val };
    const res = await fetch(`${API_BASE}/metrics/value`, {
        method: "POST",
        headers: {"Content-Type":"application/json"},
        body: JSON.stringify(body)
    });
    if (!res.ok) {
        const text = await res.text();
        alert("Ошибка: " + text);
    } else {
        showToast("Значение добавлено");
        await loadMetrics();
    }
}

// refresh chart based on selected metric(s) and period
async function refreshChart() {
    if (compareMode) {
        // use selectedCompare; if empty, show selectedMetricId only
        let ids = Array.from(selectedCompare);
        if (ids.length === 0) {
            if (selectedMetricId) ids = [String(selectedMetricId)];
        }
        await loadMultiChart(ids.map(s => Number(s)));
    } else {
        if (!selectedMetricId && metrics.length) selectedMetricId = metrics[0].id;
        if (!selectedMetricId) return;
        await loadSingleChart(selectedMetricId);
    }
}

// load single metric
async function loadSingleChart(metricId) {
    const res = await fetch(`${API_BASE}/metrics/values?user=${userId}&metric_id=${metricId}&period=${currentPeriod}`);
    const data = await res.json();
    if (!data) {return}
    const labels = data.map(d => d.date + " " + d.time);
    const values = data.map(d => d.value);
    const metric = metrics.find(m => m.id === metricId) || {name:'', color:'#22c55e'};

    renderChart([{ label: metric.name, data: values, color: metric.color }], labels);
}

// load multiple metrics
async function loadMultiChart(metricIds) {
    if (metricIds.length === 0) return;
    const idsParam = metricIds.join(",");
    const res = await fetch(`${API_BASE}/metrics/values_multi?user=${userId}&metric_ids=${idsParam}&period=${currentPeriod}`);
    const data = await res.json(); // object keyed by metric id -> array
    // Build unified labels: union of datetimes sorted
    const allDatetimes = new Set();
    for (const id of metricIds) {
        const arr = data[String(id)] || [];
        arr.forEach(it => allDatetimes.add(it.datetime));
    }
    const sorted = Array.from(allDatetimes).sort();
    const labels = sorted.map(s => {
        const t = new Date(s);
        return t.toISOString().slice(0,10) + " " + t.toISOString().slice(11,16);
    });

    const datasets = [];
    for (const id of metricIds) {
        const arr = data[String(id)] || [];
        // map datetime -> value
        const map = new Map(arr.map(it => [it.datetime, it.value]));
        const values = sorted.map(dt => map.has(dt) ? map.get(dt) : null);
        const m = metrics.find(x => x.id === id) || {name: String(id), color:'#22c55e'};
        datasets.push({ label: m.name, data: values, color: m.color });
    }

    renderChart(datasets, labels);
}

// render chart helper using Chart.js
function renderChart(datasets, labels) {
    // transform dataset objects to Chart.js datasets
    const ds = datasets.map((d, i) => ({
        label: d.label,
        data: d.data,
        borderColor: d.color || chartColor(i),
        backgroundColor: d.color || chartColor(i),
        spanGaps: true,
        tension: 0.25,
        pointRadius: 3,
        borderWidth: 2
    }));
    if (chart) chart.destroy();
    chart = new Chart(ctx, {
        type: 'line',
        data: { labels, datasets: ds },
        options: {
            interaction: { mode: 'index', intersect: false },
            scales: {
                x: { display: true, title: { display: false } },
                y: { display: true, title: { display: true, text: 'Value' } }
            },
            plugins: {
                legend: { display: true }
            }
        }
    });
    // legend area
    elLegend.innerHTML = datasets.map((d, i) => `<span style="display:inline-block;margin-right:10px"><span style="display:inline-block;width:12px;height:12px;background:${d.color};margin-right:6px;border-radius:3px"></span>${escapeHtml(d.label)}</span>`).join("");
}

function chartColor(i) {
    const palette = ["#3b82f6","#10b981","#f59e0b","#ef4444","#8b5cf6","#06b6d4","#f97316"];
    return palette[i % palette.length];
}

// utils
function escapeHtml(s){ return String(s).replace(/[&<>"']/g, (m)=>({ '&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;' }[m])); }

// period buttons
periodBtns.forEach(b => b.onclick = async (e) => {
    periodBtns.forEach(x => x.classList.remove("active"));
    e.target.classList.add("active");
    currentPeriod = e.target.dataset.period;
    await refreshChart();
});

// compare checkbox
elCompareCheckbox.onchange = async (e) => {
    compareMode = e.target.checked;
    if (!compareMode) selectedCompare.clear();
    await refreshChart();
};

// startup
loadMetrics();

// ===== PASSWORD MANAGER START =====
let passwords = [];
let masterKey = null;
let autoLockTimer = null;
const AUTO_LOCK_TIME = 5 * 60 * 1000; // 5 минут

// --- простое XOR шифрование ---
function encrypt(text, key) {
    const encoded = new TextEncoder().encode(text);
    const keyBytes = new TextEncoder().encode(key);
    const result = encoded.map((b, i) => b ^ keyBytes[i % keyBytes.length]);
    return btoa(String.fromCharCode(...result));
}

function decrypt(text, key) {
    try {
        const bytes = Uint8Array.from(atob(text), c => c.charCodeAt(0));
        const keyBytes = new TextEncoder().encode(key);
        const result = bytes.map((b, i) => b ^ keyBytes[i % keyBytes.length]);
        return new TextDecoder().decode(result);
    } catch {
        return null;
    }
}

function savePasswords() {
    const encrypted = encrypt(JSON.stringify(passwords), masterKey);
    localStorage.setItem("encrypted_passwords", encrypted);
}

function loadPasswords(key) {
    const data = localStorage.getItem("encrypted_passwords");
    if (!data) return [];
    const decrypted = decrypt(data, key);
    try {
        return JSON.parse(decrypted);
    } catch {
        return null;
    }
}

function renderPasswords() {
    const list = document.getElementById("password-list");
    list.innerHTML = "";

    if (!passwords.length) {
        list.innerHTML = "<p>Пока нет сохранённых паролей</p>";
        return;
    }

    passwords.forEach((item, idx) => {
        const div = document.createElement("div");
        div.className = "password-item";
        div.innerHTML = `
      <span class="password-name">${item.name}</span>
    <div class="password-actions">
      <button class="copy-login-btn" data-idx="${idx}">📋</button>
      <button class="copy-pas-btn" data-idx="${idx}">📋</button>
      <button class="delete-pass-btn" data-idx="${idx}">🗑️</button>
    </div>
    `;
        list.appendChild(div);
    });

    document.querySelectorAll(".copy-login-btn").forEach(btn => {
            btn.addEventListener("click", e => {
                const idx = e.target.dataset.idx;
                navigator.clipboard.writeText(passwords[idx].login);
                showToast("Login copied");
            });
        }
    );

    document.querySelectorAll(".copy-pas-btn").forEach(btn => {
            btn.addEventListener("click", e => {
                const idx = e.target.dataset.idx;
                navigator.clipboard.writeText(passwords[idx].password);
                showToast("Password copied");
            });
        }
    );

    document.querySelectorAll(".delete-pass-btn").forEach(btn =>
        btn.addEventListener("click", e => {
            const idx = e.target.dataset.idx;
            if (confirm(`Удалить "${passwords[idx].name}"?`)) {
                passwords.splice(idx, 1);
                savePasswords();
                renderPasswords();
            }
        })
    );
}

// --- авто-блокировка ---
function resetAutoLock() {
    if (autoLockTimer) clearTimeout(autoLockTimer);
    autoLockTimer = setTimeout(lockPasswords, AUTO_LOCK_TIME);
}

function lockPasswords() {
    masterKey = null;
    passwords = [];
    document.getElementById("passwords-content").style.display = "none";
    document.getElementById("password-lock").style.display = "block";
    document.getElementById("master-password").value = "";
}

// --- авторизация ---
document.getElementById("unlock-btn").addEventListener("click", () => {
    const key = document.getElementById("master-password").value.trim();
    if (!key) return alert("Введите общий пароль");

    const loaded = loadPasswords(key);
    if (loaded === null) {
        alert("Неверный общий пароль!");
        return;
    }

    document.getElementById("unlock-btn").textContent = "Разблокировать";

    masterKey = key;
    passwords = loaded;
    document.getElementById("password-lock").style.display = "none";
    document.getElementById("passwords-content").style.display = "block";
    renderPasswords();
    resetAutoLock();
});

// --- блокировка вручную ---
document.getElementById("lock-btn").addEventListener("click", () => {
    if (confirm("Заблокировать менеджер паролей?")) lockPasswords();
});

// --- блокировка вручную ---
document.getElementById("reset-master-password").addEventListener("click", () => {
    if (confirm("Вы действительно хотите удалить все пароли и заново создать мастер пароль?")) {
        localStorage.removeItem("encrypted_passwords");
        alert("Deleted!");
        saveSettings();
        document.getElementById("unlock-btn").textContent = "Установить новый пароль";
    }
    resetAutoLock();
});

if (!localStorage.getItem("encrypted_passwords")) {
    document.getElementById("unlock-btn").textContent = "Установить новый пароль";
};

// --- экспорт ---
document.getElementById("export-passwords").addEventListener("click", () => {
    const data = localStorage.getItem("encrypted_passwords");
    if (!data) return alert("Нет данных для экспорта");
    const blob = new Blob([data], { type: "text/plain" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = "passwords_encrypted.json";
    a.click();
    URL.revokeObjectURL(url);
});

// --- импорт ---
document.getElementById("import-btn").addEventListener("click", () => {
    document.getElementById("import-passwords").click();
});

document.getElementById("import-passwords").addEventListener("change", e => {
    const file = e.target.files[0];
    if (!file) return;

    const reader = new FileReader();
    reader.onload = () => {
        const imported = reader.result.trim();
        if (!confirm("Импортировать данные (старые будут заменены)?")) return;
        localStorage.setItem("encrypted_passwords", imported);
        alert("Импорт выполнен. Введите общий пароль, чтобы разблокировать.");
        lockPasswords();
    };
    reader.readAsText(file);
});

// --- сброс авто-блокировки при активности ---
["mousemove", "keydown", "click", "scroll", "touchstart"].forEach(evt =>
    document.addEventListener(evt, resetAutoLock)
);

// ===== PASSWORD MANAGER END =====

const convBaseSelect = document.getElementById("base-currency");
const convListDiv = document.getElementById("currency-list");
const addCurrencyBtn = document.getElementById("add-currency-btn");
const addCurrencyModal = document.getElementById("add-currency-modal");
const currencyToAddInput = document.getElementById("currency-to-add");
const modalAddBtn = document.getElementById("modal-add-btn");
const modalCancelBtn = document.getElementById("modal-cancel-btn");

let converterState = {
    base: "USD",
    targets: [],  // ["EUR","JPY", ...]
    rates: {}     // rates[target] = rate
};

async function loadUserCurrencies() {
    const res = await fetch(`${API_BASE}/currencies/list?user=${userId}`);
    const obj = await res.json();
    converterState.targets = obj.currencies || [];
    renderConverter();
}

async function renderConverter() {
    // базовый селект — можно выбрать базу, default - USD
    convBaseSelect.innerHTML = "";
    // например, список основных валют
    const common = ["USD","EUR","GBP","JPY","AUD","RUB","AMD"];
    common.forEach(c => {
        const opt = document.createElement("option");
        opt.value = c;
        opt.textContent = c;
        convBaseSelect.appendChild(opt);
    });
    convBaseSelect.value = converterState.base;

    // отрисуй список target валют
    convListDiv.innerHTML = "";
    for (const tgt of converterState.targets) {
        const row = document.createElement("div");
        row.className = "currency-row";
        row.innerHTML = `
      <img class="currency-flag" src="https://flagsapi.com/${tgt.slice(0,2)}/flat/32.png" alt="">
      <span class="currency-label">${tgt}</span>
      <input type="number" step="any" class="currency-input" data-currency="${tgt}">
    `;
        convListDiv.appendChild(row);
    }

    // навесим события
    convBaseSelect.onchange = async () => {
        converterState.base = convBaseSelect.value;
        await updateRatesAndConvert();
    };

    convListDiv.querySelectorAll(".currency-input").forEach(inp => {
        inp.oninput = () => {
            const tgt = inp.dataset.currency;
            const val = parseFloat(inp.value);
            if (isNaN(val)) return;
            // пересчитать все остальные
            for (const other of converterState.targets) {
                if (other === tgt) continue;
                const otherInp = convListDiv.querySelector(`.currency-input[data-currency="${other}"]`);
                const rate_tgt = converterState.rates[tgt] || converterState.rates[tgt.toLowerCase()];
                const rate_other = converterState.rates[other] || converterState.rates[other.toLowerCase()];
                // formula: other_value = val * (rate_other / rate_tgt)
                if (rate_tgt && rate_other) {
                    otherInp.value = (val * (rate_other / rate_tgt)).toFixed(4);
                }
            }
        };
    });

    await updateRatesAndConvert();
}

async function updateRatesAndConvert() {
    if (converterState.targets.length === 0) return;
    const targetCsv = converterState.targets.join(",");
    const res = await fetch(`${API_BASE}/currencies/rates?base=${converterState.base}&target=${targetCsv}`);
    const obj = await res.json();
    converterState.rates = obj.rates || {};
    // если есть уже введённое значение в одном поле, триггерим пересчёт
    const anyInp = convListDiv.querySelector(".currency-input");
    if (anyInp && anyInp.value) {
        anyInp.dispatchEvent(new Event("input"));
    }
}

// открыть модалку добавить
addCurrencyBtn.onclick = () => {
    addCurrencyModal.classList.remove("hidden");
};
modalCancelBtn.onclick = () => {
    addCurrencyModal.classList.add("hidden");
};
modalAddBtn.onclick = async () => {
    const code = currencyToAddInput.value.trim().toUpperCase();
    if (!code) return;
    await fetch(`${API_BASE}/currencies/add?user=${userId}`, {
        method: "POST",
        headers: {"Content-Type":"application/json"},
        body: JSON.stringify({ code })
    });
    addCurrencyModal.classList.add("hidden");
    currencyToAddInput.value = "";
    await loadUserCurrencies();
};

// удаление — можно реализовать кнопкой рядом с валютой

loadUserCurrencies();


class ImagePopup {
    constructor() {
        this.selectElement = document.getElementById('bg-preset');
        this.popup = document.getElementById('imagePopup');
        this.confirmPopup = document.getElementById('confirmDeletePopup');
        this.imagesGrid = document.getElementById('imagesGrid');
        this.selectedImage = null;
        this.imageToDelete = null;

        // Замените на реальный ID пользователя
        this.userId = 'testuser'; // или получите из вашей системы

        this.init();
    }

    init() {
        // Создаем кнопку для открытия попапа, если её нет
        if (!document.querySelector('.open-popup-btn')) {
            const openBtn = document.createElement('button');
            openBtn.className = 'open-popup-btn';
            openBtn.textContent = 'Выбрать фон';
            this.selectElement.parentNode.insertBefore(openBtn, this.selectElement.nextSibling);

            openBtn.addEventListener('click', () => this.openPopup());
        } else {
            document.querySelector('.open-popup-btn').addEventListener('click', () => this.openPopup());
        }

        // Закрытие основного попапа
        this.popup.querySelector('.close-btn').addEventListener('click', () => this.closePopup());
        this.popup.querySelector('.cancel-btn').addEventListener('click', () => this.closePopup());

        // Подтверждение выбора
        this.popup.querySelector('.confirm-btn').addEventListener('click', () => this.confirmSelection());

        // Закрытие по клику на оверлей
        this.popup.addEventListener('click', (e) => {
            if (e.target === this.popup) {
                this.closePopup();
            }
        });

        // Управление попапом подтверждения удаления
        document.getElementById('cancelDeleteBtn').addEventListener('click', () => this.closeConfirmDelete());
        document.getElementById('confirmDeleteBtn').addEventListener('click', () => this.deleteImage());

        // Закрытие по ESC
        document.addEventListener('keydown', (e) => {
            if (e.key === 'Escape') {
                if (this.confirmPopup.classList.contains('active')) {
                    this.closeConfirmDelete();
                } else if (this.popup.classList.contains('active')) {
                    this.closePopup();
                }
            }
        });
    }

    openPopup() {
        this.loadImages();
        this.popup.classList.add('active');
        document.body.style.overflow = 'hidden';
    }

    closePopup() {
        this.popup.classList.remove('active');
        document.body.style.overflow = '';
        this.selectedImage = null;
    }

    loadImages() {
        this.imagesGrid.innerHTML = '';
        const options = this.selectElement.querySelectorAll('option');

        options.forEach((option, index) => {
            if (index === 0) return; // Пропускаем первый option

            const imageItem = this.createImageItem(option);
            this.imagesGrid.appendChild(imageItem);
        });
    }

    createImageItem(option) {
        const imageItem = document.createElement('div');
        imageItem.className = 'image-item loading';
        imageItem.dataset.value = option.value;
        imageItem.dataset.src = option.textContent;
        imageItem.dataset.id = option.value;

        // Создаем спиннер загрузки
        const spinner = document.createElement('div');
        spinner.className = 'loading-spinner';
        imageItem.appendChild(spinner);

        // Создаем кнопку удаления
        const deleteBtn = document.createElement('button');
        deleteBtn.className = 'delete-btn';
        deleteBtn.innerHTML = '×';
        deleteBtn.title = 'Удалить изображение';
        deleteBtn.addEventListener('click', (e) => {
            e.stopPropagation(); // Предотвращаем выбор картинки при клике на крестик
            this.showConfirmDelete(imageItem);
        });
        imageItem.appendChild(deleteBtn);

        // Создаем подпись
        const label = document.createElement('div');
        label.className = 'image-label';
        label.textContent = this.getImageName(option.textContent);
        imageItem.appendChild(label);

        // Загружаем изображение
        this.loadImagePreview(option.textContent, imageItem);

        // Обработчик выбора
        imageItem.addEventListener('click', () => this.selectImage(imageItem));

        return imageItem;
    }

    loadImagePreview(src, container) {
        const img = new Image();
        img.onload = () => {
            container.classList.remove('loading');
            container.innerHTML = '';
            container.appendChild(img);

            // Добавляем кнопку удаления обратно
            const deleteBtn = document.createElement('button');
            deleteBtn.className = 'delete-btn';
            deleteBtn.innerHTML = '×';
            deleteBtn.title = 'Удалить изображение';
            deleteBtn.addEventListener('click', (e) => {
                e.stopPropagation();
                this.showConfirmDelete(container);
            });
            container.appendChild(deleteBtn);

            // Добавляем подпись обратно
            const label = document.createElement('div');
            label.className = 'image-label';
            label.textContent = this.getImageName(src);
            container.appendChild(label);
        };

        img.onerror = () => {
            container.classList.remove('loading');
            container.innerHTML = '<div style="display: flex; align-items: center; justify-content: center; height: 100%; color: #666; font-size: 12px;">Ошибка загрузки</div>';

            // Добавляем кнопку удаления даже при ошибке
            const deleteBtn = document.createElement('button');
            deleteBtn.className = 'delete-btn';
            deleteBtn.innerHTML = '×';
            deleteBtn.title = 'Удалить изображение';
            deleteBtn.addEventListener('click', (e) => {
                e.stopPropagation();
                this.showConfirmDelete(container);
            });
            container.appendChild(deleteBtn);
        };

        img.src = src;
        img.alt = this.getImageName(src);
    }

    getImageName(path) {
        return path.split('/').pop() || 'Изображение';
    }

    selectImage(imageItem) {
        // Снимаем выделение со всех элементов
        this.imagesGrid.querySelectorAll('.image-item').forEach(item => {
            item.classList.remove('selected');
        });

        // Выделяем выбранный элемент
        imageItem.classList.add('selected');
        this.selectedImage = imageItem;
    }

    confirmSelection() {
        if (this.selectedImage) {
            // Обновляем значение select
            this.selectElement.value = this.selectedImage.dataset.value;

            // Можно добавить кастомное событие
            const event = new CustomEvent('imageSelected', {
                detail: {
                    value: this.selectedImage.dataset.value,
                    src: this.selectedImage.dataset.src
                }
            });
            this.selectElement.dispatchEvent(event);

            console.log('Выбрано изображение:', this.selectedImage.dataset.src);
            saveBackground();
        }

        this.closePopup();
    }

    showConfirmDelete(imageItem) {
        this.imageToDelete = imageItem;
        const imageName = this.getImageName(imageItem.dataset.src);
        document.getElementById('confirmDeleteMessage').textContent =
            `Вы уверены, что хотите удалить изображение "${imageName}"? Это действие нельзя отменить.`;
        this.confirmPopup.classList.add('active');
    }

    closeConfirmDelete() {
        this.confirmPopup.classList.remove('active');
        this.imageToDelete = null;
    }

    async deleteImage() {
        if (!this.imageToDelete) return;

        const imageId = this.imageToDelete.dataset.id;
        const imageItem = this.imageToDelete;

        // Показываем состояние удаления
        imageItem.classList.add('deleting');
        imageItem.innerHTML = '<div class="deleting-spinner"></div>';

        try {
            // Вызываем API для удаления
            const res = await fetch(`/api/resagerhelper/settings/background?user=${this.userId}&id=${imageId}`, {
                method: 'DELETE'
            });

            if (res.ok) {
                // Удаляем элемент из DOM
                imageItem.remove();

                // Удаляем соответствующий option из select
                const optionToRemove = this.selectElement.querySelector(`option[value="${imageId}"]`);
                if (optionToRemove) {
                    optionToRemove.remove();
                }

                // Сбрасываем выбор если удалили выбранное изображение
                if (this.selectedImage === imageItem) {
                    this.selectedImage = null;
                    this.selectElement.value = '';
                }

                console.log('Изображение успешно удалено');
            } else {
                throw new Error('Ошибка при удалении');
            }
        } catch (error) {
            console.error('Ошибка при удалении изображения:', error);
            // Восстанавливаем картинку при ошибке
            this.loadImagePreview(imageItem.dataset.src, imageItem);
            imageItem.classList.remove('deleting');

            // Показываем сообщение об ошибке (можно заменить на красивый toast)
            alert('Ошибка при удалении изображения');
        }

        this.closeConfirmDelete();
    }
}

// Инициализация попапа при загрузке страницы
document.addEventListener('DOMContentLoaded', () => {
    new ImagePopup();
});

const addPassBtn = document.getElementById("add-password-btn");
const addModal = document.getElementById("add-password-modal");

// добавление
addPassBtn.onclick = () => addModal.classList.remove("hidden");
document.getElementById("cancel-password-btn").onclick = () => addModal.classList.add("hidden");
document.getElementById("save-password-btn").onclick = () => {
    const name = document.getElementById("password-name").value.trim();
    const value = document.getElementById("password-value").value.trim();
    const login = document.getElementById("password-login").value.trim();
    const desc = document.getElementById("pass-description").value.trim();
    if (!name || !value) return alert("Название и пароль обязательны");

    passwords.push({ name: name, password: value, description: desc, login: login });
    savePasswords();
    renderPasswords();
    document.getElementById("password-name").value = "";
    document.getElementById("password-value").value = "";
    resetAutoLock();

    addModal.classList.add("hidden");
};

// =============== LINKS ===============

let unlockedFolders = {};

let savedLinks = [];

function loadLinks() {
    const raw = localStorage.getItem("saved_links");
    savedLinks = raw ? JSON.parse(raw) : [];
    renderLinks();
}

function saveLinks() {
    localStorage.setItem("saved_links", JSON.stringify(savedLinks));
}

function renderLinks() {
    const list = document.getElementById("links-list");
    if (!list) {
        return;
    }
    list.innerHTML = "";

    if (!savedLinks.length) {
        list.innerHTML = "<p>Список пуст</p>";
        return;
    }

    savedLinks.forEach((item, idx) => {
        const row = document.createElement("div");
        row.className = "link-row";

        row.innerHTML = `
      <div class="link-row-body">
        <b>${item.name}</b><br>
        <small>${item.url}</small>
      </div>
      <div>
        <button data-open="${idx}">🌐</button>
        <button data-del="${idx}">🗑️</button>
      </div>
    `;

        list.appendChild(row);
    });

    // Открыть во фрейме
    document.querySelectorAll("[data-open]").forEach(btn => {
        btn.onclick = () => {
            const idx = btn.dataset.open;
            openLinkFrame(savedLinks[idx].url);
        };
    });

    // Удалить ссылку
    document.querySelectorAll("[data-del]").forEach(btn => {
        btn.onclick = () => {
            const idx = btn.dataset.del;
            if (!confirm(`Удалить ссылку "${savedLinks[idx].name}"?`)) return;
            savedLinks.splice(idx, 1);
            saveLinks();
            renderLinks();
        };
    });
}

// Добавить
document.getElementById("add-link-btn").onclick = () => {
    const name = document.getElementById("link-name").value.trim();
    const url = document.getElementById("link-url").value.trim();

    if (!name || !url) return alert("Введите название и URL");

    savedLinks.push({ name, url });
    saveLinks();
    renderLinks();

    document.getElementById("link-name").value = "";
    document.getElementById("link-url").value = "";
};

// Открыть ссылку во фрейме
// Открытие ссылки во фрейме (теперь принимает URL и path)
function openLinkFrame(url, path = null) {
    if (path) bumpUsage(path);
    const frame = document.getElementById("link-frame");
    const overlay = document.getElementById("link-frame-overlay");

    frame.src = url;
    overlay.classList.remove("hidden");
}

// Закрыть фрейм
document.getElementById("close-frame-btn").onclick = () => {
    const overlay = document.getElementById("link-frame-overlay");
    const frame = document.getElementById("link-frame");

    frame.src = "about:blank";
    overlay.classList.add("hidden");
};

// Инициализация
loadLinks();

// ==== LINKS TREE FUNCTIONALITY ====

let linksTree = [];

async function loadLinksTree() {
    // const raw = localStorage.getItem("links_tree");
    // linksTree = raw ? JSON.parse(raw) : [];
    linksTree = await loadLinksTreeSmart();
    ensurePinnedFields(linksTree);
    renderLinksTree();
}

function saveLinksTree() {
    ensurePinnedFields(linksTree);
    localStorage.setItem("links_tree", JSON.stringify(linksTree));
}

// ===================== API SYNC =====================

// Сохранение одной ссылки или папки на сервере
async function apiSaveLink(node) {
    if (linkStoreLocally) return; // работаем только локально
    try {
        await fetch(`${apiBaseV3}/links?userId=${userId}`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(node),
        });
    } catch (err) {
        console.error("Ошибка сохранения ссылки в API:", err);
    }
}

// Удаление элемента (по url)
async function apiDeleteLink(url) {
    if (linkStoreLocally) return;
    try {
        await fetch(`${apiBaseV3}/links/delete?userId=${userId}&url=${encodeURIComponent(url)}`, {
            method: "DELETE",
        });
    } catch (err) {
        console.error("Ошибка удаления ссылки в API:", err);
    }
}

// Получение всех ссылок пользователя с сервера
function flattenLinks2(tree, parentPath = "root") {
    const flat = [];
    console.log("flattenLinks2", parentPath);
    for (const node of tree) {
        if (node.type === "link") {
            const item = {
                userId,
                name: node.name?.trim() || "",
                url: node.url?.trim() || "",
                description: node.description || "",
                faviconUrl: node.faviconUrl || "",
                faviconImage: node.faviconImage || "",
                thumbnail: node.thumbnail || "",
                note: node.note || "",
                content: node.content || "",
                status: node.status || 0,
                pinned: !!node.pinned,
                usage: node.usage || 0,
                tags: Array.isArray(node.tags) ? node.tags : [],
                path: parentPath,
                createdAt: node.createdAt || new Date().toISOString(),
                updatedAt: new Date().toISOString(),
            };
            flat.push(item);
        } else if (node.type === "folder" && Array.isArray(node.children)) {
            const folderPath = `${parentPath}.${sanitizePath(node.name)}`;
            flat.push(...flattenLinks2(node.children, folderPath));
        }
    }

    return flat;
}

// function sanitizePath(name = "") {
//     return name.replace(/[^\w\d_-]+/g, "_").toLowerCase() || "untitled";
// }

function sanitizePath(name = "") {
    if (!name) return "untitled";
    // Транслитерация (для кириллицы, латиницы с диакритикой и т.п.)
    const translitMap = {
        "а": "a", "б": "b", "в": "v", "г": "g", "д": "d", "е": "e", "ё": "yo", "ж": "zh", "з": "z", "и": "i", "й": "j",
        "к": "k", "л": "l", "м": "m", "н": "n", "о": "o", "п": "p", "р": "r", "с": "s", "т": "t", "у": "u", "ф": "f",
        "х": "h", "ц": "ts", "ч": "ch", "ш": "sh", "щ": "sch", "ъ": "", "ы": "y", "ь": "", "э": "e", "ю": "yu", "я": "ya"
    };

    return name
        .split("")
        .map(ch => {
            const lower = ch.toLowerCase();
            // ASCII латиница, цифры и символы
            if (/[A-Za-z0-9_-]/.test(ch)) return ch.toLowerCase();
            if (translitMap[lower]) return translitMap[lower];
            // Пробелы — в подчёркивания
            if (/\s/.test(ch)) return "_";
            // Остальные символы -> код
            const code = ch.codePointAt(0).toString(16).padStart(4, "0");
            return `UUU${code}`;
        })
        .join("")
        .replace(/_+/g, "_")
        .replace(/^_+|_+$/g, "") || "untitled";
}

async function uploadLocalLinksToAPI() {
    const localLinks = JSON.parse(localStorage.getItem("links_tree") || "[]");
    if (!localLinks.length) return alert("Локальные ссылки отсутствуют");

    if (!confirm("Выгрузить все локальные ссылки в API?")) return;

    try {
        const flatLinks = flattenLinks2(localLinks);
        console.log("🟡 Подготовлено к выгрузке:", flatLinks.length, "записей");
        console.log(flatLinks.slice(0, 3)); // покажем первые три

        const res = await fetch(`${apiBaseV3}/links/sync?userId=${userId}`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(flatLinks),
        });

        const data = await res.json();
        alert(`✅ Синхронизировано ${data.count || flatLinks.length} элементов`);
    } catch (err) {
        console.error("Ошибка выгрузки в API:", err);
        alert("Ошибка выгрузки в API");
    }
}

// ====================================================

function renderLinksTree() {
    const container = document.getElementById("links-tree");
    container.innerHTML = "";

    const showFavorites = localStorage.getItem("links-show-favorites") === "true";
    const showTop10 = localStorage.getItem("links-show-top10") === "true";
    const flat = flattenLinksWithPath(linksTree);
    const favs = flat.filter(x => x.node && x.node.pinned);

    if (showFavorites) {
        // 1) ⭐ Избранное
        container.appendChild(renderVirtualFolder(
            "⭐ Избранное",
            favs.map(f => renderLink(f.node, f.path)),
            "favOpen"
        ));
    }

    if (showTop10) {
        // 2) 🔥 Топ-10 по usage (исключим дубликаты избранного — можно и не исключать)
        const top = flat
            .filter(x => x.node && (x.node.usage || 0) > 0)
            .sort((a, b) => (b.node.usage || 0) - (a.node.usage || 0))
            .slice(0, 10);
        container.appendChild(renderVirtualFolder(
            "🔥 Топ-10",
            top.map(t => renderLink(t.node, t.path)),
            "topOpen"
        ));
    }

    // 3) Обычное дерево
    linksTree.forEach((item, idx) => container.appendChild(renderNode(item, [idx])));

    updateLinksStats();
}

// “виртуальная папка” (как обычная, но без resolveNode)
function renderVirtualFolder(title, childrenEls, prefKey) {
    const wrap = document.createElement("div");
    wrap.className = "folder";

    const header = document.createElement("div");
    header.className = "folder-header";
    header.style.cursor = "pointer";

    const open = !!linkPrefs[prefKey];
    const icon = open ? "📂" : "📁";
    header.innerHTML = `${icon} <b>${title}</b>`;
    header.onclick = () => {
        linkPrefs[prefKey] = !linkPrefs[prefKey];
        savePrefs();
        renderLinksTree();
    };

    wrap.appendChild(header);

    if (open) {
        const childrenDiv = document.createElement("div");
        childrenDiv.className = "folder-children";
        if (childrenEls.length === 0) {
            const empty = document.createElement("div");
            empty.style.opacity = "0.7";
            empty.style.padding = "4px 0";
            empty.textContent = "Пусто";
            childrenDiv.appendChild(empty);
        } else {
            childrenEls.forEach(el => childrenDiv.appendChild(el));
        }
        wrap.appendChild(childrenDiv);
    }
    return wrap;
}

function renderNode(node, path) {
    if (node.type === "folder") return renderFolder(node, path);
    return renderLink(node, path);
}

/* ---------- FOLDER ---------- */
let folderAwaitingPasswordPath = null;

function renderFolder(folder, path) {
    const wrapper = document.createElement("div");
    wrapper.className = "folder";

    const header = document.createElement("div");
    header.className = "folder-header";

    const icon = folder.open ? "📂" : "📁";

    const nameSpan = document.createElement("span");
    nameSpan.innerHTML = `${icon} <b>${folder.name}</b>`;
    nameSpan.onclick = () => {
        const pathStr = path.join(",");

        // Папка защищена
        if (folder.password) {
            if (!unlockedFolders[pathStr]) {
                folderAwaitingPasswordPath = pathStr;
                document.getElementById("folder-password-input").value = "";
                document.getElementById("folder-password-modal").classList.remove("hidden");
                return;
            }
        }

        // Переключение open
        folder.open = !folder.open;

        // Если папку закрывают → забываем пароль
        if (!folder.open && folder.password) {
            unlockedFolders[pathStr] = false;
        }

        saveLinksTree();
        renderLinksTree();
    };

    const settingsBtn = document.createElement("span");
    settingsBtn.className = "folder-settings-btn";
    settingsBtn.textContent = "⚙️";
    settingsBtn.onclick = (e) => {
        e.stopPropagation();
        openFolderEdit(path);
    };

    header.appendChild(nameSpan);
    header.appendChild(settingsBtn);
    wrapper.appendChild(header);

    if (folder.open) {
        const childrenDiv = document.createElement("div");
        childrenDiv.className = "folder-children";

        const addBtn = document.createElement("button");
        addBtn.textContent = "+ 🔗";
        addBtn.className = "add-btn-light add-new-link-btn";
        addBtn.onclick = (e) => {
            e.stopPropagation();
            currentTargetFolder = folder;
            openAddLinkModal();
        };

        const addAFolderBtn = document.createElement("button");
        addAFolderBtn.textContent = "+ 📂";
        addAFolderBtn.className = "add-btn-light add-new-link-btn add-folder-btn";
        addAFolderBtn.onclick = (e) => {
            e.stopPropagation();
            openAddFolderModal(path);
        };

        childrenDiv.appendChild(addBtn);
        childrenDiv.appendChild(addAFolderBtn);

        console.log("render folder", folder);

        if (folder.children) {
            folder.children.forEach((child, idx) => {
                childrenDiv.appendChild(renderNode(child, [...path, idx]));
            });
        }
        wrapper.appendChild(childrenDiv);
    }

    return wrapper;
}

document.getElementById("folder-password-ok").onclick = () => {
    const pass = document.getElementById("folder-password-input").value;
    const { node } = resolveNode(folderAwaitingPasswordPath);

    if (node.password === pass) {
        unlockedFolders[folderAwaitingPasswordPath] = true;

        document.getElementById("folder-password-modal").classList.add("hidden");
        node.open = true;
        renderLinksTree();
    } else {
        alert("Неверный пароль");
    }
};


document.getElementById("folder-password-cancel").onclick = () => {
    folderAwaitingPasswordPath = null;
    document.getElementById("folder-password-modal").classList.add("hidden");
};


let currentFolderPath = null;

function openFolderEdit(path) {
    currentFolderPath = path;
    const { node } = resolveNode(path);

    document.getElementById("edit-folder-name").value = node.name;
    document.getElementById("edit-folder-password").value = node.password || "";

    document.getElementById("edit-folder-modal").classList.remove("hidden");
}

document.getElementById("edit-folder-save").onclick = () => {
    const { node } = resolveNode(currentFolderPath);

    node.name = document.getElementById("edit-folder-name").value.trim();
    let pass = document.getElementById("edit-folder-password").value.trim();
    if (pass) node.password = pass;

    saveLinksTree();
    renderLinksTree();

    document.getElementById("edit-folder-modal").classList.add("hidden");
};

document.getElementById("edit-folder-delete").onclick = () => {
    const { parent, index } = resolveNode(currentFolderPath);
    if (!confirm("Удалить папку со всем содержимым?")) return;

    parent.splice(index, 1);
    saveLinksTree();
    renderLinksTree();

    document.getElementById("edit-folder-modal").classList.add("hidden");
};

/* ---------- LINK ---------- */

function renderLink(link, path) {
    const row = document.createElement("div");
    row.className = "link-row";

    const textDiv = document.createElement("div");
    textDiv.innerHTML = `
    <b>${link.name}</b>
    <small>${link.url}</small>
  `;
    if (link.tags && link.tags.length) {
        const tagRow = document.createElement("div");
        tagRow.className = "tag-row";

        link.tags.forEach(tag => {
            const span = document.createElement("span");
            span.className = "tag";
            span.textContent = tag;

            // <<< ДОБАВЛЯЕМ КЛИК ПО ТЕГУ >>>
            span.onclick = (e) => {
                e.stopPropagation();
                filterByTag(tag);
            };

            tagRow.appendChild(span);
        });

        textDiv.appendChild(tagRow);
    }


    // Клик по текстовой части → редактор ссылки
    textDiv.onclick = (e) => {
        e.stopPropagation();
        openLinkEdit(path);
    };

    const btn = document.createElement("div");
    btn.className = "link-btn-icon";
    btn.onclick = (e) => {
        e.stopPropagation();
        openLinkFrame(link.url, path);
    };

    row.appendChild(textDiv);
    row.appendChild(btn);
    return row;
}


let currentEditingLinkPath = null;

function listFolders(tree = linksTree, prefix = "", result = [], path = []) {
    for (let i = 0; i < tree.length; i++) {
        const node = tree[i];
        if (node.type === "folder") {
            result.push({ name: prefix + node.name, path: [...path, i] });
            listFolders(node.children, prefix + node.name + " / ", result, [...path, i]);
        }
    }
    return result;
}

function openLinkEdit(path) {
    currentEditingLinkPath = path;
    const { node } = resolveNode(path);
    const select = document.getElementById("edit-link-folder");
    select.innerHTML = "";

    const folders = listFolders();
    const currentPath = path.slice(0, -1).join(",");

    // избранное
    document.getElementById("edit-link-pinned").checked = !!node.pinned;

    folders.forEach(f => {
        const opt = document.createElement("option");
        opt.value = f.path.join(",");
        opt.textContent = f.name;
        if (opt.value === currentPath) opt.selected = true;
        select.appendChild(opt);
    });

    document.getElementById("edit-link-name").value = node.name;
    document.getElementById("edit-link-url").value = node.url;
    document.getElementById("edit-link-tags").value = node.tags?.join(", ") || "";
    document.getElementById("edit-link-modal").classList.remove("hidden");
}

let editLinkName = document.getElementById("edit-link-name");
let editLinkUrl = document.getElementById("edit-link-url");
let editLinkTags = document.getElementById("edit-link-tags");

document.getElementById("edit-link-save").onclick = () => {
    const { parent, index, node } = resolveNode(currentEditingLinkPath);

    node.name   = editLinkName.value.trim();
    node.url    = editLinkUrl.value.trim();
    node.pinned = document.getElementById("edit-link-pinned").checked;
    node.tags   = editLinkTags.value.split(",").map(t => t.trim()).filter(Boolean);

    // записываем статистику тегов
    recordTags(node.tags);

    // перенос по папке (если менялась)
    const newFolderPath = document.getElementById("edit-link-folder").value;
    const oldFolderPath = currentEditingLinkPath.slice(0, -1).join(",");
    if (newFolderPath !== oldFolderPath) {
        parent.splice(index, 1);
        const { node: targetFolder } = resolveNode(newFolderPath);
        targetFolder.children.push(node);
    }

    saveLinksTree();
    if (!linkStoreLocally) apiSaveLink(node);
    renderLinksTree();
    closeModal("edit-link-modal");
};


document.getElementById("edit-link-delete").onclick = () => {
    const { parent, index } = resolveNode(currentEditingLinkPath);

    if (!confirm("Удалить ссылку?")) return;

    parent.splice(index, 1);
    if (!linkStoreLocally) apiDeleteLink(node.url);
    saveLinksTree();
    renderLinksTree();

    document.getElementById("edit-link-modal").classList.add("hidden");
};

document.getElementById("edit-link-copy").onclick = () => {
    const { node } = resolveNode(currentEditingLinkPath);
    copyText(node.url, currentEditingLinkPath);       // usage++
};

document.getElementById("edit-link-open").onclick = () => {
    const { node } = resolveNode(currentEditingLinkPath);
    openLinkFrame(node.url, currentEditingLinkPath); // usage++
};

// linksTree: корневой массив дерева ссылок
// path: либо строка "0,1,2", либо массив [0,1,2]
function resolveNode(path) {
    const indices = Array.isArray(path)
        ? path
        : String(path).split(",").map(Number);

    let parent = linksTree;
    for (let i = 0; i < indices.length - 1; i++) {
        const idx = indices[i];
        const node = parent[idx];
        if (!node || node.type !== "folder" || !Array.isArray(node.children)) {
            throw new Error("Неверный путь для resolveNode: " + path);
        }
        parent = node.children;
    }

    const index = indices[indices.length - 1];
    const node = parent[index];

    if (!node) {
        throw new Error("Узел не найден для пути: " + path);
    }

    return { parent, index, node };
}

document.getElementById("upload-links-to-api-btn").onclick = async (e) => {
    e.stopPropagation();
    await syncFoldersToAPI();
    await uploadLocalLinksToAPI();
};

/* ---------- HELPERS ---------- */

function getNodeByPath(pathStr) {
    const path = pathStr.split(",").map(Number);
    let node = linksTree;
    for (let i = 0; i < path.length - 1; i++) node = node[path[i]].children;
    return { parent: node, index: path[path.length - 1] };
}

function deleteNode(pathStr) {
    const { parent, index } = getNodeByPath(pathStr);
    if (confirm("Удалить?")) {
        parent.splice(index, 1);
        saveLinksTree();
        renderLinksTree();
    }
}

function addItemTo(pathStr) {
    const { parent, index } = getNodeByPath(pathStr);
    const folder = parent[index];
    currentTargetFolder = folder;
    document.getElementById("add-link-modal").classList.remove("hidden");
}

/* ---------- MODALS ---------- */

let currentTargetFolder = null;

document.getElementById("save-new-link").onclick = () => {
    const name = document.getElementById("new-link-name").value.trim();
    const url  = document.getElementById("new-link-url").value.trim();
    const tags = document.getElementById("new-link-tags").value
        .split(",")
        .map(t => t.trim())
        .filter(t => t.length > 0);

    currentTargetFolder.children.push({
        type: "link",
        name, url,
        pinned: false,
        usage: 0,
        tags
    });

    recordTags(tags); // учёт тегов
    saveLinksTree();
    renderLinksTree();

    // очистить поля
    document.getElementById("new-link-name").value = "";
    document.getElementById("new-link-url").value = "";
    document.getElementById("new-link-tags").value = "";

    closeModal("add-link-modal");

    if (!linkStoreLocally) apiSaveLink(currentTargetFolder.children.at(-1));

    updateLinksStats();
};

/* ---------- ROOT ADD ---------- */

document.getElementById("add-folder-btn").onclick = (e) => {
    e.stopPropagation();
    document.getElementById("add-folder-modal").classList.remove("hidden");
};

function openAddFolderModal(parentPath = []) {
    currentFolderPath = parentPath;
    console.log("openAddFolderModal",currentFolderPath);
    document.getElementById("new-folder-name").value = "";
    document.getElementById("add-folder-modal").classList.remove("hidden");
}

document.getElementById("save-new-link").onclick = async () => {
    const name = document.getElementById("new-link-name").value.trim();
    const url  = document.getElementById("new-link-url").value.trim();
    const tags = document.getElementById("new-link-tags").value
        .split(",")
        .map(t => t.trim())
        .filter(t => t.length > 0);

    if (!url) return alert("Введите ссылку");

    // 🧩 Определяем путь для новой ссылки
    const folderChain = getFolderChain(currentTargetFolder); // цепочка папок от корня
    const path = buildPathFromChain(folderChain);

    const newLink = {
        type: "link",
        name,
        url,
        pinned: false,
        usage: 0,
        tags,
        path, // добавляем путь
        createdAt: new Date().toISOString(),
        updatedAt: new Date().toISOString(),
    };

    // добавляем в локальное дерево
    if (!currentTargetFolder.children) currentTargetFolder.children = [];
    currentTargetFolder.children.push(newLink);

    recordTags(tags);
    saveLinksTree();
    renderLinksTree();

    // очистка полей
    document.getElementById("new-link-name").value = "";
    document.getElementById("new-link-url").value = "";
    document.getElementById("new-link-tags").value = "";
    closeModal("add-link-modal");
    updateLinksStats();

    // 💾 если хранение не локальное — отправляем в API
    if (!linkStoreLocally) {
        try {
            await apiSaveLink(newLink);
        } catch (err) {
            console.error("Ошибка сохранения ссылки в API:", err);
            alert("Ошибка сохранения в API");
        }
    }
};

function getFolderChain(node) {
    const chain = [];
    let current = node;

    while (current) {
        if (current.type === "folder") {
            chain.unshift({
                name: current.name,
                code: current.code || sanitizePath(current.name)
            });
        }
        current = current.parent || null;
    }

    if (!chain.length) chain.push({ name: "root", code: "root" });
    return chain;
}
// function buildPathFromChain(chain = []) {
//     return chain.map(f => f.code || sanitizePath(f.name)).join(".");
// }



document.getElementById("add-link-btn").onclick = (e) => {
    e.stopPropagation();
    currentTargetFolder = { children: linksTree };
    document.getElementById("add-link-modal").classList.remove("hidden");
};

function closeModal(id) {
    document.getElementById(id).classList.add("hidden");
}

document.getElementById("edit-link-cancel").onclick = () => {
    closeModal("edit-link-modal");
};

document.getElementById("edit-folder-cancel").onclick = () => {
    closeModal("edit-folder-modal");
};


function openAddLinkModal() {
    document.getElementById("new-link-name").value = "";
    document.getElementById("new-link-url").value  = "";
    document.getElementById("new-link-tags").value = "";
    renderTagSuggestions("create");
    document.getElementById("add-link-modal").classList.remove("hidden");
}

/* ---------- IFRAME ---------- */

// УТИЛИТА: инкремент usage для ссылки по path
function bumpUsage(path) {
    try {
        const { node } = resolveNode(path);
        node.usage = (node.usage || 0) + 1;
        saveLinksTree();
    } catch (_) {}
    updateLinksStats();
}

document.getElementById("close-frame-btn").onclick = () => {
    document.getElementById("link-frame-overlay").classList.add("hidden");
    document.getElementById("link-frame").src = "about:blank";
};

/* ---------- COPY ---------- */

// Копирование (тоже с учётом path)
function copyText(text, path = null) {
    if (path) bumpUsage(path);
    navigator.clipboard.writeText(text);
}

/* ---------- FAV ---------- */

// предпочтения отображения
const PREFS_KEY = "links_prefs";
let linkPrefs = JSON.parse(localStorage.getItem(PREFS_KEY) || "{}");
if (typeof linkPrefs.favOpen === "undefined") linkPrefs.favOpen = true;
if (typeof linkPrefs.topOpen === "undefined") linkPrefs.topOpen = true;

function savePrefs() {
    localStorage.setItem(PREFS_KEY, JSON.stringify(linkPrefs));
}

function flattenLinksWithPath(tree, acc = [], path = []) {
    console.log("flattenLinksWithPath", tree);
    if (!tree) return path;
    for (let i = 0; i < tree.length; i++) {
        const node = tree[i];
        const p = [...path, i];
        if (node.type === "link") {
            acc.push({ node, path: p });
        } else if (node.type === "folder") {
            flattenLinksWithPath(node.children, acc, p);
        }
    }
    return acc;
}

/* ---------- EXPORT ---------- */

document.getElementById("export-links-btn").onclick = () => {
    const blob = new Blob([JSON.stringify(linksTree, null, 2)], { type: "application/json" });
    const url = URL.createObjectURL(blob);

    const a = document.createElement("a");
    a.href = url;
    a.download = "links_backup.json";
    a.click();

    URL.revokeObjectURL(url);
};

/* ---------- IMPORT ---------- */

// === Заполнение списка папок для импорта ===
function populateImportFolderSelect() {
    const select = document.getElementById("import-links-folder-select");
    select.innerHTML = "";

    const optRoot = document.createElement("option");
    optRoot.value = "";
    optRoot.textContent = "— В корень —";
    select.appendChild(optRoot);

    const folders = listFolders(); // уже используемая в коде функция
    folders.forEach(f => {
        const opt = document.createElement("option");
        opt.value = f.path.join(",");
        opt.textContent = f.name;
        select.appendChild(opt);
    });
}

document.getElementById("import-links-btn").onclick = () => {
    document.getElementById("import-file-input").click();
};

document.getElementById("import-links-btn-by-text").onclick = async () => {
    const text = document.getElementById("import-links-textarea").value.trim();
    const getTitles = document.getElementById("import-links-by-text_get_web_head").checked;
    const smartTags = document.getElementById("import-links-by-text_smart_tags").checked;
    const folderPath = document.getElementById("import-links-folder-select").value;
    const commonTagsInput = document.getElementById("import-links-common-tags").value.trim();
    const commonTags = commonTagsInput ? commonTagsInput.split(",").map(t => t.trim()).filter(Boolean) : [];

    if (!text) return alert("Введите текст со ссылками");

    let imported = [];

    // 1️⃣ Попробовать JSON
    try {
        const data = JSON.parse(text);
        if (Array.isArray(data)) {
            imported = data.map((item, i) => ({
                type: "link",
                name: item.name || item.title || `link-${i + 1}`,
                url: item.url || item.link || "",
                tags: (item.tags || []).map(t => t.trim()),
            })).filter(x => x.url);
        }
    } catch {
        // 2️⃣ Разбор текстовых форматов
        const lines = text.split(/\r?\n/).map(l => l.trim()).filter(Boolean);

        for (let i = 0; i < lines.length; i++) {
            let line = lines[i];
            let tags = [];

            // формат с тегами в квадратных скобках [tag1,tag2]
            const tagMatch = line.match(/\[([^\]]+)\]/);
            if (tagMatch) {
                tags = tagMatch[1].split(/[,;]/).map(t => t.trim());
                line = line.replace(tagMatch[0], "").trim();
            }

            // заголовок + \n + ссылка
            if (i < lines.length - 1 && isUrl(lines[i + 1])) {
                imported.push({ type: "link", name: line, url: lines[i + 1], tags });
                i++;
                continue;
            }

            // заголовок и ссылка в одной строке
            const inline = line.match(/(.+?)\s+(https?:\/\/\S+)/);
            if (inline) {
                imported.push({ type: "link", name: inline[1].trim(), url: inline[2].trim(), tags });
                continue;
            }

            // ссылка - заголовок
            const reverse = line.match(/(https?:\/\/\S+)\s*[-–—]?\s*(.+)?/);
            if (reverse) {
                imported.push({
                    type: "link",
                    url: reverse[1].trim(),
                    name: reverse[2]?.trim() || "",
                    tags,
                });
                continue;
            }

            // просто ссылка
            if (isUrl(line)) {
                const domain = new URL(line).hostname.replace(/^www\./, "");
                imported.push({
                    type: "link",
                    name: `${domain} #${imported.length + 1}`,
                    url: line,
                    tags,
                });
            }
        }
    }

    // 3️⃣ Получаем <title> из сайта (если включено)
    if (getTitles) {
        for (let link of imported) {
            try {
                const resp = await fetch(link.url);
                const html = await resp.text();
                const title = html.match(/<title[^>]*>(.*?)<\/title>/i)?.[1];
                if (title) link.name = title.trim();
            } catch {
                console.warn("Ошибка получения title:", link.url);
            }
        }
    }

    // 4️⃣ Умный подбор тегов
    if (smartTags && imported.length) {
        const allExistingLinks = flattenLinks(linksTree);

        for (let link of imported) {
            const similar = allExistingLinks.find(existing => {
                if (!existing.url) return false;
                const host1 = new URL(link.url).hostname;
                const host2 = new URL(existing.url).hostname;
                const titleSim = fuzzyMatch(link.name, existing.name);
                return host1 === host2 || titleSim > 0.6;
            });

            if (similar && Array.isArray(similar.tags) && similar.tags.length) {
                link.tags = Array.from(new Set([...(link.tags || []), ...similar.tags]));
            }
        }
    }

    // 5️⃣ Добавляем общие теги
    if (commonTags.length) {
        for (let link of imported) {
            link.tags = Array.from(new Set([...(link.tags || []), ...commonTags]));
        }
    }

    if (!imported.length) return alert("Не удалось импортировать ни одной ссылки");

    // 6️⃣ Добавляем в дерево
    if (folderPath) {
        const { node } = resolveNode(folderPath);
        if (!node.children) node.children = [];
        node.children.push(...imported);
    } else {
        linksTree.push(...imported);
    }

    saveLinksTree();
    renderLinksTree();
    populateImportFolderSelect();

    alert(`Импортировано ${imported.length} ссылок`);
    document.getElementById("import-links-textarea").value = "";
    document.getElementById("import-links-common-tags").value = "";
};


// ===== Вспомогательные функции =====
function isUrl(s) {
    try {
        const u = new URL(s);
        return /^https?:/.test(u.protocol);
    } catch {
        return false;
    }
}

function flattenLinks(tree) {
    const result = [];
    for (const item of tree) {
        if (item.type === "link") result.push(item);
        if (item.type === "folder" && Array.isArray(item.children)) {
            result.push(...flattenLinks(item.children));
        }
    }
    return result;
}

// Простое “fuzzy” сравнение строк
function fuzzyMatch(a, b) {
    a = a.toLowerCase();
    b = b.toLowerCase();
    if (!a || !b) return 0;

    const aWords = a.split(/\W+/);
    const bWords = b.split(/\W+/);

    let matches = 0;
    for (const aw of aWords) {
        if (aw.length < 3) continue;
        if (bWords.some(bw => bw.includes(aw) || aw.includes(bw))) matches++;
    }

    return matches / Math.max(aWords.length, bWords.length);
}

document.getElementById("import-file-input").onchange = (e) => {
    const file = e.target.files[0];
    if (!file) return;

    const reader = new FileReader();
    reader.onload = (ev) => {
        try {
            linksTree = JSON.parse(ev.target.result);
            saveLinksTree();
            renderLinksTree();
            alert("Импорт успешно выполнен!");
        } catch {
            alert("Ошибка: неправильный формат файла");
        }
    };
    reader.readAsText(file);
};

/* ---------- INIT ---------- */

loadLinksTree();
populateImportFolderSelect();
document.getElementById("links-search").oninput = (e) => {
    const q = e.target.value.trim().toLowerCase();
    renderLinksTreeFiltered(q);
};
function filterTree(tree, query) {
    let result = [];

    for (const item of tree) {
        if (item.type === "link") {
            const match =
                item.name.toLowerCase().includes(query) ||
                item.url.toLowerCase().includes(query) ||
                (item.tags && item.tags.some(t => t.toLowerCase().includes(query)));

            if (match) result.push(item);
        }

        if (item.type === "folder") {
            const childMatches = filterTree(item.children, query);

            const matchSelf = item.name.toLowerCase().includes(query);

            if (matchSelf || childMatches.length > 0) {
                result.push({
                    ...item,
                    open: true, // раскрываем найденные папки
                    children: childMatches
                });
            }
        }
    }

    return result;
}

function renderLinksTreeFiltered(query) {
    if (!query) return renderLinksTree();

    const filtered = filterTree(linksTree, query);
    const container = document.getElementById("links-tree");
    container.innerHTML = "";

    filtered.forEach((node, idx) =>
        container.appendChild(renderNode(node, ["filtered", idx]))
    );
}

function filterByTag(tag) {
    const query = tag.toLowerCase();

    const filtered = filterTree(linksTree, query);
    const container = document.getElementById("links-tree");

    document.getElementById("links-search").value = "#" + tag;

    container.innerHTML = "";
    filtered.forEach((node, idx) =>
        container.appendChild(renderNode(node, ["filtered", idx]))
    );
}

function ensureAllProtectedFoldersStartClosed(tree) {
    for (const item of tree) {
        if (item.type === "folder") {
            if (item.password) item.open = false;
            ensureAllProtectedFoldersStartClosed(item.children);
        }
    }
}

ensureAllProtectedFoldersStartClosed(linksTree);

document.getElementById("folder-generate-share").onclick = () => {
    const { node } = resolveNode(currentFolderPath);

    function collectLinks(folder, lines = []) {
        for (const child of folder.children) {
            if (child.type === "link") {
                lines.push(`${child.name}: ${child.url}`);
            } else if (child.type === "folder") {
                lines.push(`[${child.name}]`);
                collectLinks(child, lines);
            }
        }
        return lines;
    }

    const text = collectLinks(node).join("\n");
    document.getElementById("folder-share-text").value = text;
    document.getElementById("folder-share-block").style.display = "block";
};

document.getElementById("folder-share-copy").onclick = () => {
    navigator.clipboard.writeText(document.getElementById("folder-share-text").value);
    alert("Скопировано!");
};

document.getElementById("folder-share-send").onclick = () => {
    const text = document.getElementById("folder-share-text").value;
    tg.sendData(text)
    fetch("/api/resagerhelper/share_folder", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ text }),
    }).then(() => alert("Отправлено в API!"));
};

const TAG_RECENT_KEY = "links_tag_recent";
const TAG_COUNT_KEY  = "links_tag_counts";

function getRecentTags() {
    try { return JSON.parse(localStorage.getItem(TAG_RECENT_KEY) || "[]"); }
    catch { return []; }
}
function getTagCounts() {
    try { return JSON.parse(localStorage.getItem(TAG_COUNT_KEY) || "{}"); }
    catch { return {}; }
}
function setRecentTags(arr) {
    localStorage.setItem(TAG_RECENT_KEY, JSON.stringify(arr.slice(0, 20)));
}
function setTagCounts(obj) {
    localStorage.setItem(TAG_COUNT_KEY, JSON.stringify(obj));
}

// записываем использование тегов
function recordTags(tags) {
    if (!tags || !tags.length) return;
    // recent
    const recent = getRecentTags();
    for (const t of tags) {
        const idx = recent.indexOf(t);
        if (idx !== -1) recent.splice(idx, 1);
        recent.unshift(t);
    }
    setRecentTags(recent);

    // counts
    const counts = getTagCounts();
    for (const t of tags) counts[t] = (counts[t] || 0) + 1;
    setTagCounts(counts);
}

function ensurePinnedFields(tree) {
    for (const item of tree) {
        if (item.type === "link") {
            if (typeof item.pinned === "undefined") item.pinned = false;
            if (typeof item.usage === "undefined") item.usage = 0;
        } else if (item.type === "folder" && Array.isArray(item.children)) {
            ensurePinnedFields(item.children);
        }
    }
}

function renderTagSuggestions(mode /* "create" | "edit" */) {
    const recent = getRecentTags();
    const counts = getTagCounts();
    const top = Object.keys(counts)
        .sort((a,b) => (counts[b]||0) - (counts[a]||0))
        .slice(0, 10);

    const boxId = mode === "create" ? "create-tags-suggest" : "edit-tags-suggest";
    const input = mode === "create" ? document.getElementById("new-link-tags") : document.getElementById("edit-link-tags");
    const box = document.getElementById(boxId);
    if (!box || !input) return;

    box.innerHTML = "";

    const sec1 = document.createElement("div");
    if (recent.length) {
        sec1.innerHTML = `<span class="sec-title">Последние:</span>` +
            recent.map(t => `<span class="chip" data-t="${t}">${t}</span>`).join("");
        box.appendChild(sec1);
    }

    const sec2 = document.createElement("div");
    if (top.length) {
        sec2.innerHTML = `<span class="sec-title">Популярные:</span>` +
            top.map(t => `<span class="chip" data-t="${t}">${t}</span>`).join("");
        box.appendChild(sec2);
    }

    box.onclick = (e) => {
        const t = e.target.dataset?.t;
        if (!t) return;
        // добавляем тэг в поле ввода, без дублей
        const arr = input.value.split(",").map(s => s.trim()).filter(Boolean);
        if (!arr.includes(t)) arr.push(t);
        input.value = arr.join(", ");
    };
}


function updateLinksStats() {
    const flat = flattenLinks(linksTree);
    const totalLinks = flat.length;
    const totalUsage = flat.reduce((sum, l) => sum + (l.usage || 0), 0);

    document.getElementById("links-stats").innerHTML = `
    <span>🔗 <b>${totalLinks}</b></span>
    <span>📈 <b>${totalUsage}</b></span>
  `;
}

function initViewOptions() {
    const fav = document.getElementById("links-show-favorites");
    const top = document.getElementById("links-show-top10");

    // загрузка состояния
    fav.checked = localStorage.getItem("links-show-favorites") === "true";
    top.checked = localStorage.getItem("links-show-top10") === "true";

    fav.onchange = () => {
        localStorage.setItem("links-show-favorites", fav.checked);
        renderLinksTree();
    };
    top.onchange = () => {
        localStorage.setItem("links-show-top10", top.checked);
        renderLinksTree();
    };
}
initViewOptions();

document.getElementById("folder-export-btn").onclick = () => {
    const { node } = resolveNode(currentFolderPath);
    if (!node || node.type !== "folder") {
        return alert("Ошибка: не выбрана папка для экспорта");
    }

    // Рекурсивная функция для копирования структуры
    function cloneFolder(folder) {
        const clone = {
            type: folder.type,
            name: folder.name,
            open: folder.open,
            password: folder.password || "",
            children: []
        };

        for (const child of folder.children || []) {
            if (child.type === "link") {
                clone.children.push({
                    type: "link",
                    name: child.name,
                    url: child.url,
                    pinned: !!child.pinned,
                    usage: child.usage || 0,
                    tags: child.tags || []
                });
            } else if (child.type === "folder") {
                clone.children.push(cloneFolder(child));
            }
        }

        return clone;
    }

    const exported = cloneFolder(node);
    const blob = new Blob([JSON.stringify(exported, null, 2)], { type: "application/json" });

    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = `${node.name.replace(/\s+/g, "_")}_export.json`;
    a.click();
    URL.revokeObjectURL(a.href);
};

// (async () => {
//     if (!linkStoreLocally) {
//         try {
//             const res = await fetch(`${apiBaseV3}/links?userId=${userId}`);
//             const remote = await res.json();
//             const local = JSON.parse(localStorage.getItem("links_tree") || "[]");
//             const remoteCount = Array.isArray(remote) ? remote.length : 0;
//             const localCount = Array.isArray(local) ? local.length : 0;
//
//             if (localCount > remoteCount) {
//                 if (confirm(`Найдено ${localCount - remoteCount} несохранённых ссылок. Выгрузить их в API?`)) {
//                     uploadLocalLinksToAPI();
//                 }
//             }
//         } catch (err) {
//             console.warn("Проверка синхронизации не удалась:", err);
//         }
//     }
// })();

// folderChain: массив объектов папок от корня до текущей { name, code }.
// code вычисляем один раз и храним в папке.
function buildPathFromChain(folderChain) {
    const codes = folderChain.map(f => f.code || sanitizePath(f.name));
    const full = ["root", ...codes].join(".");
    return full;
}

async function syncFoldersToAPI() {
    const localTree = JSON.parse(localStorage.getItem("links_tree") || "[]");
    const folders = [];

    function walk(tree, parentPath = "root") {
        for (const node of tree) {
            if (node.type === "folder") {
                // создаём уникальный code — если нет, генерируем
                const code = node.code || sanitizePath(node.name);
                const path = `${parentPath}.${code}`;
                const folderData = {
                    userId,
                    name: node.name,
                    code,
                    path,
                    open: !!node.open,
                    password: node.password || "",
                    color: node.color || "",
                    settings: node.settings || {},
                };
                folders.push(folderData);

                if (node.children?.length) walk(node.children, path);
            }
        }
    }

    walk(localTree);

    if (!folders.length) return alert("Нет папок для синхронизации");

    if (!confirm(`Выгрузить ${folders.length} папок в API?`)) return;

    try {
        const res = await fetch(`${apiBaseV3}/folders/sync?userId=${userId}`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(folders),
        });
        const data = await res.json();
        alert(`✅ Синхронизировано ${data.count || folders.length} папок`);
    } catch (err) {
        console.error("Ошибка выгрузки папок:", err);
        alert("Ошибка выгрузки папок в API");
    }
}

/**
 * Загружает дерево ссылок/папок — из localStorage или с сервера
 * @returns {Promise<Array>} дерево ссылок и папок
 */
async function loadLinksTreeSmart() {
    try {
        // если включено локальное хранение
        if (linkStoreLocally === true) {
            const localRaw = localStorage.getItem("links_tree");
            if (!localRaw) {
                console.warn("⚠️ Локальное хранилище пустое");
                return [];
            }
            const tree = JSON.parse(localRaw);
            console.log("📂 Загружено из localStorage:", tree);
            return tree;
        }

        // иначе — из API
        const res = await fetch(`${apiBaseV3}/links/tree?userId=${userId}`);
        if (!res.ok) throw new Error(`Ошибка API (${res.status})`);
        const data = await res.json();

        console.log("🌐 Загружено из API:", data);

        // при необходимости можно сохранить копию локально
        localStorage.setItem("links_tree_backup", JSON.stringify(data));

        return data;
    } catch (err) {
        console.error("Ошибка загрузки дерева:", err);
        alert("Ошибка при загрузке данных ссылок. Проверьте соединение с API.");
        return [];
    }
}

document.getElementById("save-new-folder").onclick = async () => {
    const name = document.getElementById("new-folder-name").value.trim();
    if (!name) return alert("Введите имя папки");

    // генерируем уникальный code и путь
    const code = sanitizePath(name);

    const folder = {
        type: "folder",
        name,
        code,
        open: false,
        children: [],
        password: "",
        color: "",
        settings: {},
    };

    // определяем путь для новой папки
    const chain = getFolderChain(currentTargetFolder);
    const path = buildPathFromChain([...chain, folder]);
    folder.path = path;
    console.log("save-new-folder click",currentFolderPath, "path",path, "chain",chain, "folder", folder);

    // добавляем в дерево
    if (currentFolderPath && currentFolderPath.length) {
        console.log("save-new-folder click",currentFolderPath);
        const { node } = resolveNode(currentFolderPath);
        console.log("save-new-folder node",node);
        if (!node.children) node.children = [];
        node.children.push(folder);
    } else {
        linksTree.push(folder);
    }

    saveLinksTree();
    renderLinksTree();
    closeModal("add-folder-modal");

    // если хранение не локальное — отправляем в API
    if (!linkStoreLocally) {
        try {
            await apiSaveFolder(folder);
        } catch (err) {
            console.error("Ошибка сохранения папки в API:", err);
            alert("Ошибка сохранения папки на сервере");
        }
    }

    // очищаем текущий путь
    currentFolderPath = [];
};

async function apiSaveFolder(folder) {
    const res = await fetch(`${apiBaseV3}/folders?userId=${userId}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(folder),
    });
    if (!res.ok) throw new Error(`Ошибка API (${res.status})`);
    return await res.json();
}

