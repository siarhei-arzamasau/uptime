package monitor

import (
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var authorityPattern = regexp.MustCompile(`(?i)^https?://(\[[0-9a-f:.]+\]|[a-z0-9.-]+)(?::([0-9]+))?(?:[/?]|$)`)
var domainLabelPattern = regexp.MustCompile(`(?i)^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
var numericLabelPattern = regexp.MustCompile(`(?i)^(?:[0-9]+|0x[0-9a-f]*)$`)

// Use the same explicit authority syntax as the browser adapter. IDNs must
// arrive in ASCII (punycode) form; paths and query strings may contain Unicode.
func validURL(value string) bool {
	if len(value) > 2048 || !utf8.ValidString(value) || strings.ContainsFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) || r == '\uFEFF' }) || strings.ContainsAny(value, "\\#") {
		return false
	}
	parts := authorityPattern.FindStringSubmatch(value)
	if parts == nil {
		return false
	}
	if parts[2] != "" {
		port, err := strconv.ParseUint(parts[2], 10, 16)
		if err != nil || port > 65535 {
			return false
		}
	}
	host := parts[1]
	if strings.HasPrefix(host, "[") {
		if !strings.Contains(host, ":") || net.ParseIP(strings.Trim(host, "[]")) == nil {
			return false
		}
	} else {
		domain := strings.TrimSuffix(host, ".")
		labels := strings.Split(domain, ".")
		if len(domain) > 253 {
			return false
		}
		for _, label := range labels {
			if !domainLabelPattern.MatchString(label) {
				return false
			}
		}
		if numericLabelPattern.MatchString(labels[len(labels)-1]) && (host != domain || net.ParseIP(host) == nil) {
			return false
		}
	}
	u, err := url.Parse(value)
	if err != nil || u.User != nil || u.Hostname() == "" {
		return false
	}
	// net/url does not reject malformed escapes in RawQuery.
	_, err = url.QueryUnescape(u.RawQuery)
	return err == nil
}
