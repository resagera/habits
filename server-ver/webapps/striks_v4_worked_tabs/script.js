const user = "testuser";
const apiBase = "/api/resagerhelper";
const daysShort = ["Пн","Вт","Ср","Чт","Пт","Сб","Вс"];
let categories = [];
let marks = [];
let editingCategory = null;

const trackerTab = document.getElementById("trackerTab");
const checksTab = document.getElementById("checksTab");
const tabTrackerBtn = document.getElementById("tab-tracker");
const tabChecksBtn = document.getElementById("tab-checks");

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


document.getElementById("addCategory").onclick = async () => {
    const name = document.getElementById("newCategory").value.trim();
    if (!name) return;
    categories.push({ name, color: "#22c55e" });
    await saveCategories();
    render();
};

async function saveCategories() {
    await fetch(`${apiBase}/set_categories?user=${user}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ categories: categories.map(c => c.name) }),
    });
}

async function loadData() {
    const res = await fetch(`${apiBase}/get?user=${user}`);
    const data = await res.json();
    categories = data.categories || [];
    marks = data.marks || [];
    render();
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

function render() {
    const container = document.getElementById("categories");
    container.innerHTML = "";
    const weeks = getLast10Weeks();

    categories.forEach(cat => {
        const catMarks = marks.filter(m => m.category === cat.name).map(m => m.date);
        const color = cat.color || "#22c55e";

        const catDiv = document.createElement("div");
        catDiv.className = "flex flex-col gap-1";

        const header = document.createElement("div");
        header.className = "flex justify-between items-center mb-1";
        header.innerHTML = `
      <span class="font-semibold">${cat.name}</span>
      <button class="menu-btn text-gray-400 hover:text-white">⋮</button>
    `;
        catDiv.appendChild(header);

        const grid = document.createElement("div");
        grid.className = "flex";

        weeks.forEach(week => {
            const col = document.createElement("div");
            col.className = "flex flex-col gap-1 mr-1";
            week.forEach(date => {
                const isMarked = catMarks.includes(date);
                const cell = document.createElement("div");
                cell.className = "w-5 h-5 rounded cursor-pointer border border-gray-700";
                cell.style.backgroundColor = isMarked ? color : "#1f2937";
                cell.onclick = () => toggleMark(cat.name, date);
                col.appendChild(cell);
            });
            grid.appendChild(col);
        });

        const daysCol = document.createElement("div");
        daysCol.className = "flex flex-col gap-1 ml-2 text-sm text-gray-400";
        daysShort.forEach(d => {
            const span = document.createElement("span");
            span.textContent = d;
            daysCol.appendChild(span);
        });
        grid.appendChild(daysCol);

        catDiv.appendChild(grid);
        container.appendChild(catDiv);

        header.querySelector(".menu-btn").onclick = (e) => openMenu(e, cat.name);
    });
}

function openMenu(e, categoryName) {
    const menu = document.createElement("div");
    menu.className = "absolute bg-gray-800 border border-gray-700 rounded shadow-lg z-50";
    menu.innerHTML = `
    <button class="block w-full text-left px-3 py-1 hover:bg-gray-700" data-act="settings">⚙ Настройки</button>
    <button class="block w-full text-left px-3 py-1 hover:bg-gray-700" data-act="delete">🗑 Удалить</button>
  `;
    document.body.appendChild(menu);

    const rect = e.target.getBoundingClientRect();
    menu.style.top = rect.bottom + "px";
    menu.style.left = rect.left + "px";

    const close = () => {
        menu.remove();
        document.removeEventListener("click", close);
    };
    setTimeout(() => document.addEventListener("click", close), 50);

    menu.onclick = async (ev) => {
        ev.stopPropagation();
        const act = ev.target.dataset.act;
        if (act === "delete") {
            categories = categories.filter(c => c.name !== categoryName);
            marks = marks.filter(m => m.category !== categoryName);
            await saveCategories();
            render();
        } else if (act === "settings") {
            openColorSettings(categoryName);
        }
        close();
    };
}

async function toggleMark(category, date) {
    await fetch(`${apiBase}/toggle?user=${user}`, {
        method: "POST",
        headers: {"Content-Type": "application/json"},
        body: JSON.stringify({category, date}),
    });
    await loadData();
}

function openColorSettings(category) {
    editingCategory = category;
    const modal = document.getElementById("colorModal");
    modal.classList.remove("hidden");
    modal.classList.add("flex");

    const current = categories.find(c => c.name === category);
    document.getElementById("colorPicker").value = current?.color || "#22c55e";
}

document.getElementById("cancelColor").onclick = () => {
    document.getElementById("colorModal").classList.add("hidden");
    document.getElementById("colorModal").classList.remove("flex");
};

document.getElementById("saveColor").onclick = async () => {
    const color = document.getElementById("colorPicker").value;
    await fetch(`${apiBase}/set_color?user=${user}`, {
        method: "POST",
        headers: {"Content-Type": "application/json"},
        body: JSON.stringify({category: editingCategory, color}),
    });
    await loadData();
    document.getElementById("colorModal").classList.add("hidden");
    showToast("Цвет сохранён ✅");
};

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


loadData();

async function loadChecks() {
    const res = await fetch(`${apiBase}/get_checks?user=${user}`);
    const data = await res.json();
    renderChecks(data);
}

function renderChecks(groups) {
    const container = document.getElementById("checkGroups");
    container.innerHTML = "";

    for (const group of groups) {
        const groupEl = document.createElement("div");
        groupEl.className = "bg-white p-4 rounded shadow relative";

        const title = document.createElement("div");
        title.className = "flex justify-between items-center mb-2";
        title.innerHTML = `
      <span class="font-semibold">${group.name}</span>
      <button class="menu-btn text-gray-600 hover:text-black">☰</button>
    `;

        // Меню группы
        const menu = document.createElement("div");
        menu.className = "absolute right-2 top-8 bg-white border rounded shadow-md hidden";
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
            await fetch(`${apiBase}/rename_check_group?user=${user}`, {
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
            await fetch(`${apiBase}/add_check_item?user=${user}`, {
                method: "POST",
                headers: {"Content-Type": "application/json"},
                body: JSON.stringify({group: group.name, name: itemName}),
            });
            await loadChecks();
            showToast("Пункт добавлен ✅");
        };

        menu.querySelector(".delete-group").onclick = async () => {
            if (!confirm(`Удалить группу "${group.name}"?`)) return;
            await fetch(`${apiBase}/delete_check_group?user=${user}`, {
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
            item.className = "flex items-center justify-between mb-1";

            item.innerHTML = `
        <label class="flex items-center gap-2 cursor-pointer">
          <input type="checkbox" ${check.done ? "checked" : ""} data-group="${group.name}" data-item="${check.name}">
          <span>${check.name}</span>
        </label>
        <button class="text-gray-500 hover:text-red-600 delete-item">✖</button>
      `;

            const checkbox = item.querySelector("input");
            checkbox.onchange = async (e) => {
                await fetch(`${apiBase}/toggle_check?user=${user}`, {
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
                await fetch(`${apiBase}/delete_check_item?user=${user}`, {
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


document.getElementById("addCheckGroup").onclick = async () => {
    const name = prompt("Введите название группы:");
    if (!name) return;
    await fetch(`${apiBase}/add_check_group?user=${user}`, {
        method: "POST",
        headers: {"Content-Type": "application/json"},
        body: JSON.stringify({name}),
    });
    await loadChecks();
    showToast("Группа добавлена ✅");
};
