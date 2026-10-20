package config

import (
	"fmt"
	"strings"
)

// ParseEnv parses .env file content: KEY=VALUE lines, optional `export `
// prefix, # comments, blank lines, single quoted (literal) and double quoted
// (escapes \n \t \r \" \\) values, and trailing ` # comment` on unquoted
// values. Later duplicate keys win. Malformed lines are reported in the
// returned error while valid lines are still returned.
func ParseEnv(content string) (map[string]string, error) {
	out := map[string]string{}
	var errs []string
	content = strings.TrimPrefix(content, "\xef\xbb\xbf")
	for i, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "export "); ok {
			line = strings.TrimSpace(rest)
		}
		key, val, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			errs = append(errs, fmt.Sprintf("line %d: expected KEY=VALUE", i+1))
			continue
		}
		v, err := parseEnvValue(strings.TrimSpace(val))
		if err != nil {
			errs = append(errs, fmt.Sprintf("line %d: %v", i+1, err))
			continue
		}
		out[key] = v
	}
	if len(errs) > 0 {
		return out, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return out, nil
}

func parseEnvValue(val string) (string, error) {
	if val == "" {
		return "", nil
	}
	switch val[0] {
	case '\'':
		end := strings.IndexByte(val[1:], '\'')
		if end < 0 {
			return "", fmt.Errorf("unterminated single quote")
		}
		return val[1 : 1+end], nil
	case '"':
		var b strings.Builder
		for i := 1; i < len(val); i++ {
			c := val[i]
			switch c {
			case '"':
				return b.String(), nil
			case '\\':
				if i+1 >= len(val) {
					return "", fmt.Errorf("unterminated double quote")
				}
				i++
				switch val[i] {
				case 'n':
					b.WriteByte('\n')
				case 't':
					b.WriteByte('\t')
				case 'r':
					b.WriteByte('\r')
				case '"', '\\':
					b.WriteByte(val[i])
				default:
					b.WriteByte('\\')
					b.WriteByte(val[i])
				}
			default:
				b.WriteByte(c)
			}
		}
		return "", fmt.Errorf("unterminated double quote")
	}
	if i := strings.Index(val, " #"); i >= 0 {
		val = val[:i]
	}
	if i := strings.Index(val, "\t#"); i >= 0 {
		val = val[:i]
	}
	return strings.TrimSpace(val), nil
}
