package scrape

import (
	"fmt"
	"net"
	"net/url"
	"path"
	"strings"
)

type Scope struct {
	seedOrigin string
	seedPath   string
}

func NewScope(seed string) (Scope, string, error) {
	u, err := url.Parse(seed)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return Scope{}, "", fmt.Errorf("invalid seed URL %q", seed)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return Scope{}, "", fmt.Errorf("unsupported seed URL scheme %q", u.Scheme)
	}
	canonicalize(u)
	seedPath := strings.TrimSuffix(u.Path, "/")
	if seedPath == "" {
		seedPath = "/"
	}
	return Scope{seedOrigin: origin(u), seedPath: seedPath}, u.String(), nil
}

func (s Scope) Resolve(baseURL, href string) (string, bool, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", false, err
	}
	reference, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return "", false, nil
	}
	u := base.ResolveReference(reference)
	canonicalize(u)
	if u.Scheme != "http" && u.Scheme != "https" {
		return u.String(), false, nil
	}
	return u.String(), s.Contains(u), nil
}

func (s Scope) Contains(u *url.URL) bool {
	if origin(u) != s.seedOrigin {
		return false
	}
	if s.seedPath == "/" {
		return true
	}
	return u.Path == s.seedPath || strings.HasPrefix(u.Path, s.seedPath+"/")
}

func canonicalize(u *url.URL) {
	u.Fragment = ""
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	host, port, err := net.SplitHostPort(u.Host)
	if err == nil && ((u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443")) {
		u.Host = host
	}
	u.Path = path.Clean("/" + strings.TrimPrefix(u.EscapedPath(), "/"))
	if decoded, err := url.PathUnescape(u.Path); err == nil {
		u.Path = decoded
	}
	u.RawPath = ""
}

func origin(u *url.URL) string { return u.Scheme + "://" + u.Host }
