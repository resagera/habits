// Package jurwater — исполнитель автоматизации заказа воды на jur.am
// (Magento 2 + кастомный модуль Jur_Checkout). Весь сценарий выполняется
// обычными HTTP-запросами с cookie-сессией, без headless-браузера.
//
// Шаги: логин → добавить N бутылей в корзину → узнать долг возвратной тары →
// сохранить адрес/время доставки → сохранить возвратную тару → (реальный
// заказ) оформить наличными. В dry-run последний шаг не выполняется.
package jurwater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const base = "https://jur.am/ru"

// Params — параметры одного запуска.
type Params struct {
	Login     string
	Password  string
	ProductID string // id товара «Макур Джур 19л» = 465
	Quantity  int    // сколько бутылей заказать (14)
	TareMode  string // "auto" — брать долг тары из CRM; "fixed" — TareQty
	TareQty   int
	TimeSlot  string // "" / "first" — первый доступный; иначе конкретное значение
	Payment   string // код метода оплаты (checkmo — наличные)
	Comment   string // комментарий к доставке
	DryRun    bool   // не оформлять заказ, дойти до последнего шага

	// Transport — необязательный транспорт сетевого выхода. Прод-сервер за
	// Cloudflare (403 с датацентр-IP), поэтому запросы к jur.am туннелируются
	// через домашний агент (резидентный IP). nil — прямой выход (для локальных
	// тестов с резидентного IP).
	Transport http.RoundTripper
}

// Step — запись пошагового лога.
type Step struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// Result — итог запуска.
type Result struct {
	Steps   []Step
	OrderID string
	Err     error
}

type runner struct {
	http  *http.Client
	steps []Step
}

var (
	formKeyRe = regexp.MustCompile(`name="form_key"[^>]*value="([^"]+)"`)
	addURLRe  = regexp.MustCompile(`checkout/cart/add/uenc/[^"'\\]+/product/\d+/?`)
	// Magento при успешном оформлении возвращает голый целочисленный id заказа.
	orderIDRe = regexp.MustCompile(`^[1-9][0-9]*$`)
)

// Run выполняет сценарий. Возвращает лог и ошибку (шаг, на котором упало).
func Run(ctx context.Context, p Params) Result {
	jar, _ := cookiejar.New(nil)
	r := &runner{http: &http.Client{
		Jar:       jar,
		Timeout:   45 * time.Second,
		Transport: p.Transport, // nil → http.DefaultTransport (прямой выход)
		// не следуем редиректам автоматически на POST-логине — код 302 = успех
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
	if p.ProductID == "" {
		p.ProductID = "465"
	}
	if p.Payment == "" {
		p.Payment = "checkmo"
	}
	orderID, err := r.run(ctx, p)
	return Result{Steps: r.steps, OrderID: orderID, Err: err}
}

func (r *runner) step(name string, ok bool, detail string) {
	r.steps = append(r.steps, Step{Name: name, OK: ok, Detail: detail})
}

// fail добавляет неуспешный шаг и возвращает ошибку.
func (r *runner) fail(name, detail string) error {
	r.step(name, false, detail)
	return fmt.Errorf("%s: %s", name, detail)
}

func (r *runner) run(ctx context.Context, p Params) (string, error) {
	// 1. сессия + form_key с некэшируемой страницы корзины
	cartHTML, code, err := r.getStatus(ctx, base+"/checkout/cart/")
	if err != nil {
		return "", r.fail("сессия", err.Error())
	}
	fk := firstMatch(formKeyRe, cartHTML)
	if fk == "" {
		return "", r.fail("сессия", sessionDiag(code, cartHTML))
	}
	r.step("сессия", true, "получен form_key")

	// 2. логин
	loginResp, err := r.postForm(ctx, base+"/customer/account/loginPost/", url.Values{
		"form_key":       {fk},
		"login[username]": {p.Login},
		"login[password]": {p.Password},
	})
	if err != nil {
		return "", r.fail("логин", err.Error())
	}
	if !r.loggedIn(ctx) {
		return "", r.fail("логин", "неверный логин или пароль (сессия не авторизована)")
	}
	_ = loginResp
	r.step("логин", true, "вход выполнен ("+p.Login+")")

	// 3. свежий form_key после логина (Magento его меняет)
	cartHTML, err = r.get(ctx, base+"/checkout/cart/")
	if err != nil {
		return "", r.fail("корзина", err.Error())
	}
	fk = firstMatch(formKeyRe, cartHTML)

	// 4. очистить корзину (чтобы количество было ровно Quantity, а не суммировалось)
	r.clearCart(ctx)

	// 5. добавить Quantity бутылей — берём add-URL со страницы товара
	prodHTML, err := r.get(ctx, base+"/eshop/water/driking-water-in-19l-12l-bottles/maqur-jur-19l")
	if err != nil {
		return "", r.fail("товар", err.Error())
	}
	addPath := firstMatch(addURLRe, prodHTML)
	if addPath == "" {
		return "", r.fail("товар", "не найдена форма добавления в корзину")
	}
	if _, err := r.postForm(ctx, base+"/"+strings.TrimPrefix(addPath, "/")+"?isAjax=1", url.Values{
		"form_key": {fk},
		"product":  {p.ProductID},
		"qty":      {itoa(p.Quantity)},
	}); err != nil {
		return "", r.fail("корзина", err.Error())
	}
	cartCount, cartName := r.cartState(ctx)
	if cartCount != p.Quantity {
		return "", r.fail("корзина", fmt.Sprintf("ожидалось %d, в корзине %d", p.Quantity, cartCount))
	}
	r.step("корзина", true, fmt.Sprintf("%s × %d", cartName, cartCount))

	// 6. конфиг чекаута: адрес, слоты времени, долг тары
	cfg, err := r.checkoutConfig(ctx)
	if err != nil {
		return "", r.fail("чекаут", err.Error())
	}
	addr := cfg.address()
	if addr == nil {
		return "", r.fail("чекаут", "у аккаунта нет адреса доставки")
	}

	// возвратная тара
	tare := p.TareQty
	if p.TareMode == "auto" || p.TareMode == "" {
		tare = r.tareDebt(ctx, addr.ID)
	}

	// время доставки: первый слот из конфигурации либо заданный
	deliveryTime := p.TimeSlot
	if deliveryTime == "" || deliveryTime == "first" {
		deliveryTime = cfg.firstTimeSlot()
	}
	deliveryDate := cfg.firstAvailableDate(time.Now())

	// 7. сохранить адрес + время доставки (шаг «Следующий» №1)
	pm, err := r.saveShipping(ctx, addr, deliveryDate, deliveryTime, p.Comment)
	if err != nil {
		return "", r.fail("время доставки", err.Error())
	}
	if !contains(pm, p.Payment) {
		return "", r.fail("время доставки", "метод оплаты '"+p.Payment+"' недоступен; есть: "+strings.Join(pm, ", "))
	}
	r.step("время доставки", true, fmt.Sprintf("%s %s", deliveryDate, deliveryTime))

	// 8. возвратная тара (шаг «Следующий» №2)
	if err := r.saveBottles(ctx, fk, tare); err != nil {
		return "", r.fail("возвратная тара", err.Error())
	}
	r.step("возвратная тара", true, fmt.Sprintf("сдать %d", tare))

	// 9. оформление заказа
	if p.DryRun {
		r.step("заказ", true, "dry-run: остановлено перед оформлением (заказ НЕ создан)")
		return "", nil
	}
	orderID, err := r.placeOrder(ctx, addr, p.Payment)
	if err != nil {
		return "", r.fail("заказ", err.Error())
	}
	r.step("заказ", true, "заказ оформлен №"+orderID)
	return orderID, nil
}

// --- HTTP-помощники ---

func (r *runner) get(ctx context.Context, u string) (string, error) {
	body, _, err := r.getStatus(ctx, u)
	return body, err
}

// getStatus — как get, но отдаёт и HTTP-код (нужен для диагностики блокировок,
// когда сайт возвращает 403/challenge-страницу без нужного контента).
func (r *runner) getStatus(ctx context.Context, u string) (string, int, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (habits-automation)")
	resp, err := r.http.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 500 {
		return "", resp.StatusCode, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return string(b), resp.StatusCode, nil
}

// sessionDiag формирует понятную причину, когда на странице корзины не нашёлся
// form_key: чаще всего это Cloudflare/anti-bot challenge вместо реальной страницы.
func sessionDiag(code int, html string) string {
	low := strings.ToLower(html)
	switch {
	case strings.Contains(low, "just a moment") || strings.Contains(low, "cf-challenge") ||
		strings.Contains(low, "cf-browser-verification") || strings.Contains(low, "attention required") ||
		(strings.Contains(low, "cloudflare") && (code == 403 || code == 503)):
		return fmt.Sprintf("не найден form_key — Cloudflare заблокировал запрос (HTTP %d). "+
			"Вероятно, IP домашнего агента попал под защиту от ботов", code)
	case code == 403 || code == 503:
		return fmt.Sprintf("не найден form_key — сайт вернул HTTP %d (защита от ботов или недоступность)", code)
	case len(strings.TrimSpace(html)) == 0:
		return fmt.Sprintf("не найден form_key — пустой ответ (HTTP %d)", code)
	default:
		return fmt.Sprintf("не найден form_key (HTTP %d, %d байт получено)", code, len(html))
	}
}

func (r *runner) postForm(ctx context.Context, u string, form url.Values) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("User-Agent", "Mozilla/5.0 (habits-automation)")
	resp, err := r.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 500 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return string(b), nil
}

func (r *runner) postJSON(ctx context.Context, u string, body any) (string, int, error) {
	buf, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(string(buf)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("User-Agent", "Mozilla/5.0 (habits-automation)")
	resp, err := r.http.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	return string(b), resp.StatusCode, nil
}

// --- логика шагов ---

func (r *runner) loggedIn(ctx context.Context) bool {
	body, err := r.get(ctx, base+"/customer/section/load/?sections=customer")
	if err != nil {
		return false
	}
	var data struct {
		Customer struct {
			Firstname string `json:"firstname"`
			Fullname  string `json:"fullname"`
		} `json:"customer"`
	}
	_ = json.Unmarshal([]byte(body), &data)
	return data.Customer.Firstname != "" || data.Customer.Fullname != ""
}

func (r *runner) cartState(ctx context.Context) (int, string) {
	body, err := r.get(ctx, base+"/customer/section/load/?sections=cart&force_new_section_timestamp=true")
	if err != nil {
		return 0, ""
	}
	var data struct {
		Cart struct {
			Count int `json:"summary_count"`
			Items []struct {
				Name string  `json:"product_name"`
				Qty  float64 `json:"qty"`
			} `json:"items"`
		} `json:"cart"`
	}
	_ = json.Unmarshal([]byte(body), &data)
	name := ""
	qty := 0
	for _, it := range data.Cart.Items {
		name = it.Name
		qty += int(it.Qty)
	}
	if qty == 0 {
		qty = data.Cart.Count
	}
	return qty, name
}

// clearCart опустошает персистентную корзину клиента, чтобы количество было
// ровно Quantity, а не суммировалось с остатком прошлых заказов. REST-эндпоинты
// `carts/mine/*` с session-авторизацией ТРЕБУЮТ заголовок X-Requested-With,
// иначе Magento отвечает 401 (без него корзина не чистилась — баг «в корзине 28/42»).
func (r *runner) clearCart(ctx context.Context) {
	body, code, err := r.getXHR(ctx, base+"/rest/ru/V1/carts/mine/items")
	if err != nil || code != 200 {
		return
	}
	var items []struct {
		ItemID int64 `json:"item_id"`
	}
	if json.Unmarshal([]byte(body), &items) != nil {
		return
	}
	for _, it := range items {
		req, _ := http.NewRequestWithContext(ctx, http.MethodDelete,
			fmt.Sprintf("%s/rest/ru/V1/carts/mine/items/%d", base, it.ItemID), nil)
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		req.Header.Set("User-Agent", "Mozilla/5.0 (habits-automation)")
		if resp, err := r.http.Do(req); err == nil {
			resp.Body.Close()
		}
	}
}

// getXHR — GET с X-Requested-With (для session-REST endpoints carts/mine/*).
func (r *runner) getXHR(ctx context.Context, u string) (string, int, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (habits-automation)")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	resp, err := r.http.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return string(b), resp.StatusCode, nil
}

func (r *runner) tareDebt(ctx context.Context, addressID int64) int {
	body, err := r.get(ctx, fmt.Sprintf("%s/crmintegration/index/bottlesdebt?address_id=%d", base, addressID))
	if err != nil {
		return 0
	}
	var d struct {
		MakurJur19 int `json:"makurJur19"`
	}
	_ = json.Unmarshal([]byte(body), &d)
	if d.MakurJur19 < 0 {
		return 0
	}
	return d.MakurJur19
}

func (r *runner) saveShipping(ctx context.Context, a *address, date, timeSlot, comment string) ([]string, error) {
	ship := a.toMap()
	ship["extension_attributes"] = map[string]any{
		"mp_delivery_date":       date,
		"mp_delivery_time":       timeSlot,
		"mp_house_security_code": "",
		"mp_delivery_comment":    comment,
	}
	bill := a.toMap()
	payload := map[string]any{"addressInformation": map[string]any{
		"shipping_address":      ship,
		"billing_address":       bill,
		"shipping_carrier_code": "flatrate",
		"shipping_method_code":  "flatrate",
	}}
	body, code, err := r.postJSON(ctx, base+"/rest/ru/V1/carts/mine/shipping-information", payload)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("HTTP %d: %s", code, trim(body, 200))
	}
	var resp struct {
		PaymentMethods []struct {
			Code string `json:"code"`
		} `json:"payment_methods"`
	}
	_ = json.Unmarshal([]byte(body), &resp)
	var codes []string
	for _, m := range resp.PaymentMethods {
		codes = append(codes, m.Code)
	}
	return codes, nil
}

func (r *runner) saveBottles(ctx context.Context, fk string, tare int) error {
	body, err := r.postForm(ctx, base+"/checkoutjur/quote/index", url.Values{
		"form_key":   {fk},
		"byuregh":    {"0"},
		"byureghpoqr": {"0"},
		"makurpoqr":  {"0"},
		"makurmec":   {itoa(tare)},
	})
	if err != nil {
		return err
	}
	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal([]byte(body), &resp)
	if !resp.Success {
		return fmt.Errorf("ответ сайта: %s", trim(body, 200))
	}
	return nil
}

func (r *runner) placeOrder(ctx context.Context, a *address, payment string) (string, error) {
	payload := map[string]any{
		"cartId":         "mine",
		"billingAddress": a.toMap(),
		"paymentMethod":  map[string]any{"method": payment},
	}
	body, code, err := r.postJSON(ctx, base+"/rest/ru/V1/carts/mine/payment-information", payload)
	if err != nil {
		return "", err
	}
	if code != 200 {
		return "", fmt.Errorf("HTTP %d: %s", code, trim(body, 300))
	}
	// Успех Magento — голый целочисленный id заказа (иногда в кавычках). Раньше
	// сюда возвращалось ЛЮБОЕ тело при 200, из-за чего шаг считался успешным,
	// даже когда заказ не создавался (тело null/false/ошибка) — «нет ошибок, но и
	// заказа нет». Проверяем, что это действительно номер заказа.
	orderID := strings.Trim(strings.TrimSpace(body), `"`)
	if orderIDRe.MatchString(orderID) {
		return orderID, nil
	}
	// 200, но тело — не номер заказа: заказ НЕ создан. Ошибка безопасна для
	// повтора/расписания (дубля заказа не будет). Прикладываем сырой ответ и
	// остаток корзины — по ним поймём, чего ждёт кастомный чекаут jur.am.
	detail := trim(body, 300)
	if detail == "" {
		detail = "пустой ответ"
	}
	if cnt, _ := r.cartState(ctx); cnt != 0 {
		detail = fmt.Sprintf("%s; в корзине осталось %d", detail, cnt)
	}
	return "", fmt.Errorf("сайт не вернул номер заказа — заказ не создан (ответ: %s)", detail)
}

// --- checkoutConfig ---

var checkoutCfgRe = regexp.MustCompile(`window\.checkoutConfig\s*=\s*(\{.*?\});`)

type address struct {
	ID        int64
	CountryID string
	RegionID  any
	Region    string
	Street    []string
	City      string
	Postcode  string
	Firstname string
	Lastname  string
	Telephone string
}

func (a *address) toMap() map[string]any {
	region := map[string]any{"region": a.Region}
	if a.RegionID != nil {
		region["region_id"] = a.RegionID
	}
	return map[string]any{
		"customer_address_id": a.ID,
		"country_id":          a.CountryID,
		"region":              a.Region,
		"region_id":           a.RegionID,
		"street":              a.Street,
		"city":                a.City,
		"postcode":            a.Postcode,
		"firstname":           a.Firstname,
		"lastname":            a.Lastname,
		"telephone":           a.Telephone,
		"save_in_address_book": 0,
	}
}

type checkoutCfg struct {
	timeSlots []string
	dateOff   map[string]bool // "dd/mm/yyyy" -> полностью недоступна
	addr      *address
}

func (r *runner) checkoutConfig(ctx context.Context) (*checkoutCfg, error) {
	html, err := r.get(ctx, base+"/checkout/")
	if err != nil {
		return nil, err
	}
	m := checkoutCfgRe.FindStringSubmatch(html)
	if m == nil {
		return nil, fmt.Errorf("не найден checkoutConfig")
	}
	var raw struct {
		CustomTime []string `json:"custom_time_checkout"`
		DateOff    []struct {
			Date   string `json:"date"`
			First  string `json:"first"`
			Second string `json:"second"`
			Third  string `json:"third"`
		} `json:"custom_date_off_checkout"`
		CustomerData struct {
			Addresses map[string]struct {
				ID        json.Number `json:"id"`
				CountryID string      `json:"country_id"`
				RegionID  any         `json:"region_id"`
				Region    struct {
					Region string `json:"region"`
				} `json:"region"`
				Street          any    `json:"street"`
				City            string `json:"city"`
				Postcode        string `json:"postcode"`
				Firstname       string `json:"firstname"`
				Lastname        string `json:"lastname"`
				Telephone       string `json:"telephone"`
				DefaultShipping any    `json:"default_shipping"`
			} `json:"addresses"`
		} `json:"customerData"`
	}
	if err := json.Unmarshal([]byte(m[1]), &raw); err != nil {
		return nil, fmt.Errorf("разбор checkoutConfig: %w", err)
	}
	cfg := &checkoutCfg{timeSlots: raw.CustomTime, dateOff: map[string]bool{}}
	for _, d := range raw.DateOff {
		if d.First == "1" && d.Second == "1" && d.Third == "1" {
			cfg.dateOff[d.Date] = true
		}
	}
	// адрес по умолчанию (или первый)
	for _, a := range raw.CustomerData.Addresses {
		id, _ := a.ID.Int64()
		ad := &address{
			ID: id, CountryID: a.CountryID, RegionID: a.RegionID, Region: a.Region.Region,
			Street: toStrings(a.Street), City: a.City, Postcode: a.Postcode,
			Firstname: a.Firstname, Lastname: a.Lastname, Telephone: a.Telephone,
		}
		if cfg.addr == nil || truthy(a.DefaultShipping) {
			cfg.addr = ad
		}
	}
	return cfg, nil
}

func (c *checkoutCfg) address() *address { return c.addr }

func (c *checkoutCfg) firstTimeSlot() string {
	for _, t := range c.timeSlots {
		if t != "" {
			return t
		}
	}
	return "09:00:00 - 14:00:00"
}

// firstAvailableDate — ближайшая дата начиная с завтра, не помеченная как
// полностью недоступная (формат "dd/mm/yyyy"), в формате "YYYY-MM-DD".
func (c *checkoutCfg) firstAvailableDate(now time.Time) string {
	for i := 1; i <= 14; i++ {
		d := now.AddDate(0, 0, i)
		key := fmt.Sprintf("%02d/%02d/%d", d.Day(), int(d.Month()), d.Year())
		if !c.dateOff[key] {
			return d.Format("2006-01-02")
		}
	}
	return now.AddDate(0, 0, 1).Format("2006-01-02")
}

// --- утилиты ---

func firstMatch(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if len(m) > 1 {
		return m[1]
	}
	if len(m) == 1 {
		return m[0]
	}
	return ""
}

func toStrings(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return []string{""}
}

func truthy(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x == "1" || x == "true"
	case float64:
		return x != 0
	}
	return false
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func trim(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n]
	}
	return s
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }
