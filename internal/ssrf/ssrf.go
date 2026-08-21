package ssrf

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

var (
	ErrBlockedScheme = errors.New("only http and https URLs are allowed")
	ErrBlockedHost   = errors.New("destination is not allowed")
	ErrTooManyRedir  = errors.New("too many redirects")
	ErrTooLarge      = errors.New("file exceeds maximum size")
)

type Options struct {
	MaxRedirects int
	MaxBytes     int64
	Timeout      time.Duration
	Allowlist    []string
	Blocklist    []string
	UserAgent    string
}

func DefaultOptions() Options {
	return Options{
		MaxRedirects: 5,
		MaxBytes:     200 << 20,
		Timeout:      60 * time.Second,
		UserAgent:    "SoundDock/0.1 (self-hosted music)",
	}
}

func hostAllowed(host string, allow, block []string) error {
	h := strings.ToLower(host)
	if h == "localhost" || strings.HasSuffix(h, ".localhost") || h == "metadata.google.internal" ||
		h == "metadata.google" || strings.Contains(h, "169.254.169.254") {
		return ErrBlockedHost
	}
	for _, b := range block {
		if strings.EqualFold(h, b) || strings.HasSuffix(h, "."+strings.ToLower(b)) {
			return ErrBlockedHost
		}
	}
	if len(allow) > 0 {
		ok := false
		for _, a := range allow {
			if strings.EqualFold(h, a) || strings.HasSuffix(h, "."+strings.ToLower(a)) {
				ok = true
				break
			}
		}
		if !ok {
			return ErrBlockedHost
		}
	}
	return nil
}

func ipBlocked(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || ip.IsInterfaceLocalMulticast() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
		if ip4[0] == 0 {
			return true
		}
	}
	if ip.To4() == nil {
		if ip.IsPrivate() {
			return true
		}
		// Unique local fc00::/7 is IsPrivate in Go 1.20+
		if ip[0] == 0xfe && ip[1]&0xc0 == 0x80 { // fe80::/10
			return true
		}
		// AWS IMDS IPv6
		if ip.Equal(net.ParseIP("fd00:ec2::254")) {
			return true
		}
	}
	return false
}

func ResolveAndValidate(ctx context.Context, host string) ([]net.IP, error) {
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	var out []net.IP
	for _, a := range ips {
		if ipBlocked(a.IP) {
			return nil, fmt.Errorf("%w: %s", ErrBlockedHost, a.IP)
		}
		out = append(out, a.IP)
	}
	if len(out) == 0 {
		return nil, ErrBlockedHost
	}
	return out, nil
}

type limited struct {
	r io.Reader
	n int64
}

func (l *limited) Read(p []byte) (int, error) {
	if l.n <= 0 {
		return 0, ErrTooLarge
	}
	if int64(len(p)) > l.n {
		p = p[:l.n]
	}
	n, err := l.r.Read(p)
	l.n -= int64(n)
	return n, err
}

// Fetch downloads a remote file with DNS pinning and private-range blocking.
func Fetch(ctx context.Context, raw string, opt Options) (io.ReadCloser, string, int64, error) {
	if opt.MaxRedirects == 0 {
		opt.MaxRedirects = 5
	}
	if opt.MaxBytes == 0 {
		opt.MaxBytes = DefaultOptions().MaxBytes
	}
	if opt.Timeout == 0 {
		opt.Timeout = DefaultOptions().Timeout
	}
	current := raw
	var lastHost string
	for hop := 0; hop <= opt.MaxRedirects; hop++ {
		u, err := url.Parse(current)
		if err != nil {
			return nil, "", 0, err
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return nil, "", 0, ErrBlockedScheme
		}
		host := u.Hostname()
		if err := hostAllowed(host, opt.Allowlist, opt.Blocklist); err != nil {
			return nil, "", 0, err
		}
		ips, err := ResolveAndValidate(ctx, host)
		if err != nil {
			return nil, "", 0, err
		}
		ip := ips[0]
		port := u.Port()
		if port == "" {
			if u.Scheme == "https" {
				port = "443"
			} else {
				port = "80"
			}
		}
		dialer := &net.Dialer{Timeout: 15 * time.Second, Control: func(network, address string, c syscall.RawConn) error {
			return nil
		}}
		transport := &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			},
			DisableCompression: true,
		}
		client := &http.Client{
			Timeout:   opt.Timeout,
			Transport: transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, current, nil)
		if err != nil {
			return nil, "", 0, err
		}
		ua := opt.UserAgent
		if ua == "" {
			ua = DefaultOptions().UserAgent
		}
		req.Header.Set("User-Agent", ua)
		req.Host = host
		lastHost = host
		resp, err := client.Do(req)
		if err != nil {
			return nil, "", 0, err
		}
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			loc := resp.Header.Get("Location")
			resp.Body.Close()
			if loc == "" {
				return nil, "", 0, ErrTooManyRedir
			}
			next, err := u.Parse(loc)
			if err != nil {
				return nil, "", 0, err
			}
			current = next.String()
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, "", 0, fmt.Errorf("remote HTTP %d", resp.StatusCode)
		}
		ct := resp.Header.Get("Content-Type")
		n := resp.ContentLength
		if n > opt.MaxBytes {
			resp.Body.Close()
			return nil, "", 0, ErrTooLarge
		}
		body := io.NopCloser(&limited{r: resp.Body, n: opt.MaxBytes})
		_ = lastHost
		return struct {
			io.Reader
			io.Closer
		}{body, resp.Body}, ct, n, nil
	}
	return nil, "", 0, ErrTooManyRedir
}

func IsIPBlocked(ip net.IP) bool { return ipBlocked(ip) }
