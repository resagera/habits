package receipts

import "strings"

// Встроенный словарь групп товаров.
//
// Это только СТАРТ: он покрывает ходовые товары, чтобы первый же чек разложился
// не пустым. Дальше главный источник — память решений пользователя, она
// перекрывает словарь. Всё, что не опознано, честно уходит в «Не разобрано» и
// видно отдельным куском диаграммы: молчаливое «Прочее» скрывало бы проблему.

// Group — группа товаров. Code хранится в finance_item_groups и связывается с
// категорией из дерева пользователя.
type Group struct {
	Code  string `json:"code"`
	Title string `json:"title"`
	Icon  string `json:"icon"`
}

var Groups = []Group{
	{Code: "food", Title: "Продукты", Icon: "🍽"},
	{Code: "alcohol", Title: "Алкоголь", Icon: "🍷"},
	{Code: "household", Title: "Бытовая химия", Icon: "🧴"},
	{Code: "tech", Title: "Техника", Icon: "🔌"},
	{Code: "services", Title: "Сервисы", Icon: "🛠"},
	{Code: "delivery", Title: "Доставка и сервис", Icon: "🚚"},
}

func GroupTitle(code string) string {
	for _, g := range Groups {
		if g.Code == code {
			return g.Title
		}
	}
	return code
}

// Порядок проверки важен: алкоголь идёт до продуктов, иначе «Beer» попадёт под
// общее правило напитков. Бытовая химия — до продуктов по той же причине
// («Soap» и «Sauce» рядом не стоят, но «Paper» и «Pepper» — почти).
var groupRules = []struct {
	code  string
	words []string
}{
	{"alcohol", []string{
		"beer", "wine", "vodka", "whisky", "whiskey", "cognac", "brandy", "rum ",
		"gin ", "liqueur", "vermouth", "champagne", "cider", "tequila", "absinthe",
		"пиво", "вино", "водка", "коньяк", "виски", "ром ", "джин", "шампанск",
		"сидр", "ликёр", "ликер",
	}},
	{"household", []string{
		"napkin", "detergent", "soap", "shampoo", "toothpaste", "tooth brush",
		"toothbrush", "toilet paper", "paper towel", "sponge", "cleaner",
		"cleaning", "bleach", "washing", "polyethylene", "garbage bag",
		"trash bag", "dishwash", "air freshener", "wet wipes", "diaper",
		"салфет", "порошок", "мыло", "шампун", "зубн", "туалетн", "губка",
		"чистящ", "моющ", "пакет", "подгузник", "стиральн",
	}},
	{"tech", []string{
		"battery", "batteries", "charger", "usb", "cable", "lamp", "bulb",
		"headphone", "adapter", "power bank", "flash drive", "батарей",
		"зарядк", "кабель", "лампочк", "наушник", "адаптер",
	}},
	{"services", []string{
		"subscription", "warranty", "insurance", "подписка", "страхов",
	}},
	{"food", []string{
		// мясо, птица, рыба
		"beef", "pork", "veal", "lamb", "chicken", "turkey", "duck", "fish",
		"salmon", "trout", "tuna", "shrimp", "sausage", "ham", "bacon", "nugget",
		"dumpling", "cutlet", "meat", "fillet", "tenderloin", "мясо", "куриц",
		"свинин", "говядин", "рыба", "колбас", "сосиск", "пельмен", "котлет",
		// молочное
		"cheese", "milk", "yogurt", "yoghurt", "kefir", "butter", "cream",
		"cottage", "сыр", "молок", "йогурт", "кефир", "масло", "сметан", "творог",
		// хлеб, крупы, бакалея
		"bread", "ciabatta", "lavash", "bun", "roll", "pasta", "spaghetti",
		"rice", "buckwheat", "flour", "sugar", "salt", "pepper", "spice",
		"sauce", "ketchup", "mayonnaise", "oil", "vinegar", "honey", "jam",
		"cereal", "oat", "хлеб", "лаваш", "булк", "макарон", "рис ", "греч",
		"мука", "сахар", "соль", "перец", "специ", "соус", "кетчуп", "майонез",
		"масл", "уксус", "мёд", "мед ", "варень", "хлопь", "крупа",
		// овощи, фрукты, зелень
		"potato", "tomato", "cucumber", "onion", "garlic", "carrot", "cabbage",
		"pepper", "lettuce", "cilantro", "coriander", "dill", "parsley", "basil",
		"greens", "vegetable", "fruit", "apple", "banana", "orange", "lemon",
		"grape", "berry", "watermelon", "melon", "avocado", "mushroom",
		"картоф", "помидор", "томат", "огурец", "огурц", "лук ", "чеснок",
		"морков", "капуст", "салат", "кинза", "укроп", "петрушк", "зелень",
		"овощ", "фрукт", "яблок", "банан", "апельсин", "лимон", "виноград",
		"ягод", "арбуз", "дын", "авокадо", "гриб",
		// напитки без алкоголя и сладкое
		"water", "juice", "cola", "lemonade", "tea", "coffee", "cocoa",
		"chocolate", "candy", "cookie", "wafer", "cake", "ice cream", "dessert",
		"вода", "сок ", "лимонад", "чай", "кофе", "какао", "шоколад", "конфет",
		"печень", "вафл", "торт", "мороженое", "десерт",
		// яйца и прочее базовое
		"egg", "яйц", "canned", "консерв",
	}},
}

// GroupWords — слова встроенного словаря для группы. Показываются на экране
// настройки: иначе непонятно, почему товар уехал именно туда.
func GroupWords(code string) []string {
	for _, rule := range groupRules {
		if rule.code == code {
			return rule.words
		}
	}
	return nil
}

// GuessGroup подбирает группу по названию. Пустая строка — не опознано; это
// нормальный исход, а не ошибка.
func GuessGroup(name string) string {
	n := " " + strings.ToLower(name) + " "
	for _, rule := range groupRules {
		for _, w := range rule.words {
			if strings.Contains(n, w) {
				return rule.code
			}
		}
	}
	return ""
}

// NameKey — нормализованное название: ключ памяти решений и истории цен.
//
// Размер и объём НЕ отбрасываются: «Beer "Kotayk" 0.5l» и «0.33l» — разные
// товары с разной ценой, и для истории цен их смешивать нельзя.
func NameKey(name string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(name))), " ")
}
