package smtpd

import (
	"context"
	"errors"
	"net"
	"strings"
)

// Упрощённая проверка SPF: механизмы ip4, ip6, a, mx, include и all.
//
// Полный RFC 7208 (макросы, exists, ptr, redirect) здесь не нужен: результат
// идёт ТОЛЬКО в спам-оценку, письмо из-за него не отклоняется. Отклонять по
// SPF нельзя — законные пересылки (списки рассылки, форварды) ломают его
// штатно, и почта от людей начала бы теряться.
//
// Ограничение на число DNS-запросов (как в RFC — 10) не роскошь: без него
// цепочка include превращается в усилитель запросов на нашем резолвере.

type spfResult string

const (
	spfPass      spfResult = "pass"
	spfFail      spfResult = "fail"
	spfSoftFail  spfResult = "softfail"
	spfNeutral   spfResult = "neutral"
	spfNone      spfResult = "none"
	spfPermError spfResult = "permerror"
	spfTempError spfResult = "temperror"
)

type spfChecker struct {
	resolver *net.Resolver
	lookups  int
}

// checkSPF — проверяет, разрешено ли домену отправлять с этого IP.
func checkSPF(ctx context.Context, resolver *net.Resolver, domain string, ip net.IP) spfResult {
	if domain == "" || ip == nil {
		return spfNone
	}
	c := &spfChecker{resolver: resolver}
	return c.check(ctx, domain, ip, 0)
}

func (c *spfChecker) check(ctx context.Context, domain string, ip net.IP, depth int) spfResult {
	if depth > 3 || c.lookups > 10 {
		return spfPermError
	}
	record, err := c.record(ctx, domain)
	if err != nil {
		return spfTempError
	}
	if record == "" {
		return spfNone
	}

	for _, term := range strings.Fields(record)[1:] { // [0] — v=spf1
		qualifier := byte('+')
		if len(term) > 0 && strings.ContainsRune("+-~?", rune(term[0])) {
			qualifier = term[0]
			term = term[1:]
		}
		name, arg, _ := strings.Cut(term, ":")
		name = strings.ToLower(name)
		// "a/24" и "mx/24" — префиксная форма без аргумента
		name, _, _ = strings.Cut(name, "/")

		matched := false
		switch name {
		case "all":
			matched = true
		case "ip4", "ip6":
			matched = matchNet(arg, ip)
		case "a":
			host := arg
			if host == "" {
				host = domain
			}
			matched = c.matchHost(ctx, host, ip)
		case "mx":
			host := arg
			if host == "" {
				host = domain
			}
			matched = c.matchMX(ctx, host, ip)
		case "include":
			if arg == "" {
				return spfPermError
			}
			if c.check(ctx, arg, ip, depth+1) == spfPass {
				matched = true
			}
		case "redirect", "exp", "ptr", "exists":
			// redirect/exists требуют полного движка, ptr давно не рекомендуют;
			// молча пропускаем — результат всё равно только для оценки
			continue
		default:
			continue
		}
		if !matched {
			continue
		}
		switch qualifier {
		case '-':
			return spfFail
		case '~':
			return spfSoftFail
		case '?':
			return spfNeutral
		default:
			return spfPass
		}
	}
	return spfNeutral
}

func (c *spfChecker) record(ctx context.Context, domain string) (string, error) {
	c.lookups++
	txts, err := c.resolver.LookupTXT(ctx, domain)
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return "", nil // домена нет или записей нет — это «none», не сбой
		}
		return "", err
	}
	for _, t := range txts {
		if strings.HasPrefix(strings.ToLower(t), "v=spf1") {
			return t, nil
		}
	}
	return "", nil
}

func (c *spfChecker) matchHost(ctx context.Context, host string, ip net.IP) bool {
	c.lookups++
	addrs, err := c.resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if a.IP.Equal(ip) {
			return true
		}
	}
	return false
}

func (c *spfChecker) matchMX(ctx context.Context, host string, ip net.IP) bool {
	c.lookups++
	mxs, err := c.resolver.LookupMX(ctx, host)
	if err != nil {
		return false
	}
	for _, mx := range mxs {
		if c.lookups > 10 {
			return false
		}
		if c.matchHost(ctx, strings.TrimSuffix(mx.Host, "."), ip) {
			return true
		}
	}
	return false
}

// matchNet сверяет IP с механизмом ip4:/ip6: — с маской или без.
func matchNet(arg string, ip net.IP) bool {
	if arg == "" {
		return false
	}
	if strings.Contains(arg, "/") {
		_, netw, err := net.ParseCIDR(arg)
		if err != nil {
			return false
		}
		return netw.Contains(ip)
	}
	return net.ParseIP(arg).Equal(ip)
}

// spfScore — вклад проверки в спам-оценку. Отсутствие записи наказывается
// слабо: у множества мелких доменов SPF просто не настроен.
func spfScore(r spfResult) (int, string) {
	switch r {
	case spfFail:
		return 4, "SPF: отправитель не разрешён доменом"
	case spfSoftFail:
		return 2, "SPF: softfail"
	case spfNone:
		return 1, "у домена отправителя нет SPF"
	default:
		return 0, ""
	}
}

// hostPortIP — IP из адреса вида "1.2.3.4:5678".
func hostPortIP(addr string) net.IP {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	return net.ParseIP(host)
}

func domainOf(email string) string {
	i := strings.LastIndex(email, "@")
	if i < 0 || i == len(email)-1 {
		return ""
	}
	return strings.ToLower(email[i+1:])
}
