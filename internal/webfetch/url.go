package webfetch

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/idna"
)

type resolvedTarget struct {
	URL      *url.URL
	Hostname string
	Port     string
	Address  netip.Addr
}

func ParseURL(rawURL string) (*url.URL, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil, errorOf(ErrorInvalidURL, rawURL, "web URL is empty")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, errorOf(ErrorInvalidURL, rawURL, "parse web URL: %v", err)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errorOf(ErrorInvalidURL, rawURL, "web URL must use http or https")
	}
	if parsed.User != nil {
		return nil, errorOf(ErrorInvalidURL, rawURL, "web URL must not contain userinfo")
	}
	hostname := canonicalHostname(parsed.Hostname())
	if hostname == "" {
		return nil, errorOf(ErrorInvalidURL, rawURL, "web URL has no hostname")
	}
	if _, literalErr := netip.ParseAddr(hostname); literalErr != nil {
		hostname, err = idna.Lookup.ToASCII(hostname)
		if err != nil {
			return nil, errorOf(ErrorInvalidURL, rawURL, "web URL hostname is invalid: %v", err)
		}
		hostname = canonicalHostname(hostname)
	}
	port := parsed.Port()
	if port != "" {
		value, parseErr := strconv.Atoi(port)
		if parseErr != nil || value < 1 || value > 65535 {
			return nil, errorOf(ErrorInvalidURL, rawURL, "web URL has invalid port %q", port)
		}
	}
	parsed.Host = hostname
	if port != "" {
		parsed.Host = net.JoinHostPort(hostname, port)
	} else if strings.Contains(hostname, ":") {
		parsed.Host = "[" + hostname + "]"
	}
	parsed.Fragment = ""
	return parsed, nil
}

func canonicalHostname(hostname string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(hostname)), ".")
}

func resolveTarget(ctx context.Context, parsed *url.URL, resolve Resolver) (resolvedTarget, error) {
	if parsed == nil {
		return resolvedTarget{}, errorOf(ErrorInvalidURL, "", "web URL is nil")
	}
	hostname := canonicalHostname(parsed.Hostname())
	addresses, err := resolveAddresses(ctx, hostname, resolve)
	if err != nil {
		var fetchError *Error
		if errors.As(err, &fetchError) {
			fetchError.URL = parsed.String()
		}
		return resolvedTarget{}, err
	}
	port := parsed.Port()
	if port == "" {
		if parsed.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return resolvedTarget{URL: parsed, Hostname: hostname, Port: port, Address: addresses[0]}, nil
}

func resolveAddresses(ctx context.Context, hostname string, resolve Resolver) ([]netip.Addr, error) {
	if literal, err := netip.ParseAddr(hostname); err == nil {
		literal = literal.Unmap()
		if restrictedAddress(literal) {
			return nil, errorOf(ErrorSSRFRejected, hostname, "web URL host %q is not a public address", hostname)
		}
		return []netip.Addr{literal}, nil
	}
	addresses, err := resolve(ctx, hostname)
	if err != nil {
		return nil, errorOf(ErrorNetwork, hostname, "resolve web URL host %q: %v", hostname, err)
	}
	if len(addresses) == 0 {
		return nil, errorOf(ErrorNetwork, hostname, "resolve web URL host %q: no addresses", hostname)
	}
	normalized := make([]netip.Addr, 0, len(addresses))
	seen := make(map[netip.Addr]struct{}, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if restrictedAddress(address) {
			return nil, errorOf(ErrorSSRFRejected, hostname, "web URL host %q resolves to restricted address %s", hostname, address)
		}
		if _, exists := seen[address]; exists {
			continue
		}
		seen[address] = struct{}{}
		normalized = append(normalized, address)
	}
	sort.Slice(normalized, func(left, right int) bool { return normalized[left].Compare(normalized[right]) < 0 })
	return normalized, nil
}

func resolveHost(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

func restrictedAddress(address netip.Addr) bool {
	address = address.Unmap()
	return !address.IsValid() || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() || address.IsUnspecified()
}

func sameHostname(left, right *url.URL) bool {
	if left == nil || right == nil {
		return false
	}
	return canonicalHostname(left.Hostname()) == canonicalHostname(right.Hostname())
}

func redirectURL(current *url.URL, location string) (*url.URL, error) {
	if current == nil {
		return nil, errors.New("redirect source URL is nil")
	}
	target, err := current.Parse(location)
	if err != nil {
		return nil, fmt.Errorf("parse redirect URL: %w", err)
	}
	return ParseURL(target.String())
}
