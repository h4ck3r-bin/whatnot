package whtnt

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"
)

func (m *Module) generateAccountUsername() string {
	first := cleanUsernamePart(m.account.firstName)
	last := cleanUsernamePart(m.account.lastName)
	if first == "" || last == "" {
		if parts := namePartsFromEmail(m.account.email); len(parts) >= 2 {
			if first == "" {
				first = parts[0]
			}
			if last == "" {
				last = parts[1]
			}
		}
	}
	if first == "" {
		first = "buyer"
	}
	if last == "" {
		last = "collector"
	}
	initial := first[:1]
	year := strconv.Itoa(1980 + rand.Intn(29))
	number4 := fmt.Sprintf("%04d", rand.Intn(10000))
	number5 := fmt.Sprintf("%05d", rand.Intn(100000))
	number6 := fmt.Sprintf("%06d", rand.Intn(1000000))
	styles := []string{
		first + last + number5,
		first + last + year + number4[:2],
		initial + last + first + number4,
		first + initial + last + number5[:3],
		last + first + number4,
		first + last[:1] + number6,
	}
	return clampUsername(styles[rand.Intn(len(styles))])
}

func cleanUsernamePart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func namePartsFromEmail(email string) []string {
	local := strings.ToLower(strings.TrimSpace(email))
	if i := strings.Index(local, "@"); i >= 0 {
		local = local[:i]
	}
	fields := strings.FieldsFunc(local, func(r rune) bool {
		return r == '.' || r == '_' || r == '-' || (r >= '0' && r <= '9')
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = cleanUsernamePart(field)
		if len(field) >= 2 {
			out = append(out, field)
		}
	}
	return out
}

func fallbackAccountUsername() string {
	return fmt.Sprintf("buyercollector%d%d", time.Now().Unix()%100000, rand.Intn(10000))
}

func clampUsername(username string) string {
	username = cleanUsernameAlphaNum(username)
	if len(username) > 24 {
		username = username[:24]
	}
	if len(username) < 6 {
		username += fmt.Sprintf("%06d", rand.Intn(1000000))
	}
	return username
}

func cleanUsernameAlphaNum(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func checkoutUsernameAdequate(username string, firstName string, lastName string) bool {
	username = cleanUsernameAlphaNum(username)
	first := cleanUsernamePart(firstName)
	last := cleanUsernamePart(lastName)
	if username == "" {
		return false
	}
	if last != "" && strings.Contains(username, last) {
		return true
	}
	if first != "" && last != "" && strings.Contains(username, first) && strings.Contains(username, last[:1]) {
		return true
	}
	if last == "" && first != "" && strings.Contains(username, first) {
		return true
	}
	return false
}
