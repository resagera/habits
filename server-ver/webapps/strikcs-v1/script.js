const tg = window.Telegram.WebApp;
const userId = tg.initDataUnsafe?.user?.id || "testuser";

let categories = [];
const today = new Date();
let marks = [];

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
        headers: {"Content-Type": "application/json"},
        body: JSON.stringify({category: cat, date: dateStr})
    });
    loadData();
}

function render() {
    const container = document.getElementById("categories");
    container.innerHTML = "";

    for (let cat of categories) {
        const div = document.createElement("div");
        div.className = "category";

        const nameDiv = document.createElement("div");
        nameDiv.className = "category-name";
        nameDiv.textContent = cat;

        const delBtn = document.createElement("span");
        delBtn.className = "delete-btn";
        delBtn.textContent = "🗑";
        delBtn.onclick = () => {
            categories = categories.filter(c => c !== cat);
            syncCategories();
            render();
        };

        nameDiv.appendChild(delBtn);
        div.appendChild(nameDiv);

        const grid = document.createElement("div");
        grid.className = "grid";

        for (let i = 0; i < 28; i++) {
            const d = new Date(today);
            d.setDate(today.getDate() - (27 - i));
            const dateStr = d.toISOString().split("T")[0];
            const cell = document.createElement("div");
            cell.className = "cell";
            if (marks.find(m => m.date === dateStr && m.category === cat)) {
                cell.classList.add("active");
            }
            cell.onclick = () => toggleDay(cat, dateStr);
            grid.appendChild(cell);
        }

        div.appendChild(grid);
        container.appendChild(div);
    }
}

async function syncCategories() {
    await fetch(`/api/resagerhelper/set_categories?user=${userId}`, {
        method: "POST",
        headers: {"Content-Type": "application/json"},
        body: JSON.stringify({categories})
    });
}

document.getElementById("add-btn").onclick = async () => {
    const input = document.getElementById("new-category");
    const val = input.value.trim();
    if (!val || categories.includes(val)) return;
    categories.push(val);
    input.value = "";
    await syncCategories();
    loadData();
};

loadData();
