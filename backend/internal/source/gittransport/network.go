package gittransport

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var hostnamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$`)
var alwaysBlocked = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/3"), netip.MustParsePrefix("168.63.129.16/32"), netip.MustParsePrefix("100.100.100.200/32"),
	netip.MustParsePrefix("fe80::/10"), netip.MustParsePrefix("ff00::/8"), netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("fd00:ec2::254/128"),
	// Deny translation/tunnelling ranges that can conceal an IPv4 destination.
	netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("64:ff9b:1::/48"), netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("2001::/32"),
}

func allowedAddress(ip netip.Addr, private bool) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || (!ip.IsGlobalUnicast() && !ip.IsLoopback()) {
		return false
	}
	for _, p := range alwaysBlocked {
		if p.Contains(ip) {
			return false
		}
	}
	internal := ip.IsPrivate() || ip.IsLoopback() || netip.MustParsePrefix("100.64.0.0/10").Contains(ip)
	return !internal || private
}
func ValidateOrigin(raw string, private bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery || u.Path != "" || u.RawPath != "" || strings.Contains(raw, "\\") {
		return fmt.Errorf("source origin must be a canonical HTTPS origin without credentials, path or query")
	}
	host := u.Hostname()
	if strings.HasSuffix(u.Host, ":") {
		return fmt.Errorf("invalid source port")
	}
	if strings.Contains(host, "%") || strings.HasSuffix(host, ".") || host != strings.ToLower(host) {
		return fmt.Errorf("invalid source hostname")
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if !allowedAddress(ip, private) {
			return fmt.Errorf("source destination is blocked by network policy")
		}
	} else {
		if len(host) > 253 || !hostnamePattern.MatchString(host) {
			return fmt.Errorf("invalid source hostname")
		}
		for _, label := range strings.Split(host, ".") {
			if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
				return fmt.Errorf("invalid source hostname")
			}
		}
	}
	if u.Port() != "" {
		p, err := strconv.Atoi(u.Port())
		if err != nil || p < 1 || p > 65535 || strconv.Itoa(p) != u.Port() {
			return fmt.Errorf("invalid source port")
		}
	}
	return nil
}
func ValidateCA(value string) error {
	if value == "" {
		return nil
	}
	if len(value) > 64<<10 {
		return fmt.Errorf("source CA bundle is too large")
	}
	count := 0
	rest := []byte(value)
	for len(strings.TrimSpace(string(rest))) > 0 {
		rest = []byte(strings.TrimSpace(string(rest)))
		if !strings.HasPrefix(string(rest), "-----BEGIN CERTIFICATE-----") {
			return fmt.Errorf("source CA must contain only PEM certificates")
		}
		block, next := pem.Decode(rest)
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			return fmt.Errorf("source CA must contain only PEM certificates")
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil || !cert.IsCA {
			return fmt.Errorf("source CA bundle contains an invalid CA certificate")
		}
		count++
		rest = next
	}
	if count == 0 {
		return fmt.Errorf("source CA bundle is empty")
	}
	return nil
}
func (c Config) Validate() error {
	if err := ValidateOrigin(c.Origin, c.AllowPrivateNetwork); err != nil {
		return err
	}
	if err := ValidateOrigin(c.APIOrigin, c.AllowPrivateNetwork); err != nil {
		return err
	}
	return ValidateCA(c.CAPEM)
}
func approvedAddresses(ctx context.Context, host string, private bool) ([]netip.Addr, error) {
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return nil, fmt.Errorf("cannot resolve source host")
	}
	for _, ip := range addresses {
		if !allowedAddress(ip, private) {
			return nil, fmt.Errorf("source destination is blocked by network policy")
		}
	}
	return addresses, nil
}

// NewHTTPClient validates every dial, connects to the already validated IP, uses
// no ambient proxy, and rejects all redirects (including same-origin redirects).
func NewHTTPClient(c Config) (*http.Client, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		roots = x509.NewCertPool()
	}
	if c.CAPEM != "" {
		roots.AppendCertsFromPEM([]byte(c.CAPEM))
	}
	api, _ := url.Parse(c.APIOrigin)
	port := api.Port()
	if port == "" {
		port = "443"
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}, ForceAttemptHTTP2: true, MaxIdleConns: 4, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, p, err := net.SplitHostPort(address)
		if err != nil || host != api.Hostname() || p != port {
			return nil, fmt.Errorf("source request escaped its approved origin")
		}
		addresses, err := approvedAddresses(ctx, host, c.AllowPrivateNetwork)
		if err != nil {
			return nil, err
		}
		var last error
		for _, ip := range addresses {
			conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), p))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		_ = last
		return nil, fmt.Errorf("cannot connect to source host")
	}
	return &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("source redirects are not permitted") }}, nil
}

// GetJSON only accepts paths constructed by adapters. Response/error bodies are
// never reflected into logs because the provider may echo a credential.
func GetJSON(ctx context.Context, client *http.Client, origin, path string, headers http.Header, out any) (http.Header, error) {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return nil, fmt.Errorf("invalid source API path")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+path, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid source API request")
	}
	req.Header = headers.Clone()
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("source API request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("source API returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil || len(data) > 2<<20 {
		return nil, fmt.Errorf("source API response exceeds limit or is unreadable")
	}
	if err := json.Unmarshal(data, out); err != nil {
		return nil, fmt.Errorf("invalid source API response")
	}
	return resp.Header, nil
}
