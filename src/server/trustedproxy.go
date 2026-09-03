package server

import (
	"net"
	"strings"
)

// alwaysTrustedCIDRs are the proxy peer ranges trusted with no operator
// configuration: loopback, RFC 1918 / RFC 4193 private, and link-local
// (AI.md PART 12 → "Trusted Proxies").
var alwaysTrustedCIDRs = []string{
	"127.0.0.0/8",
	"::1/128",
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"fc00::/7",
	"169.254.0.0/16",
	"fe80::/10",
}

// buildTrustedProxies parses the always-trusted ranges, the listen address's
// own /24 (containerized reverse-proxy sidecar pattern), and any operator
// supplied additional IPs/CIDRs into a single slice of networks. Unparseable
// entries are skipped rather than aborting startup.
func buildTrustedProxies(listenAddr string, additional []string) []*net.IPNet {
	var nets []*net.IPNet

	add := func(cidr string) {
		if _, n, err := net.ParseCIDR(cidr); err == nil {
			nets = append(nets, n)
		}
	}

	for _, c := range alwaysTrustedCIDRs {
		add(c)
	}

	// Same /24 as the configured listen address, for sidecar proxies sharing
	// a container network. Bare IPs are widened to /24 (IPv4) or /64 (IPv6).
	if ip := parseListenIP(listenAddr); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			add(v4.Mask(net.CIDRMask(24, 32)).String() + "/24")
		} else {
			add(ip.Mask(net.CIDRMask(64, 128)).String() + "/64")
		}
	}

	for _, entry := range additional {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			add(entry)
			continue
		}
		if ip := net.ParseIP(entry); ip != nil {
			if ip.To4() != nil {
				add(entry + "/32")
			} else {
				add(entry + "/128")
			}
			continue
		}
		// DNS names are resolved once at startup; each resolved address is
		// added as a host route.
		if addrs, err := net.LookupIP(entry); err == nil {
			for _, ip := range addrs {
				if ip.To4() != nil {
					add(ip.String() + "/32")
				} else {
					add(ip.String() + "/128")
				}
			}
		}
	}

	return nets
}

// parseListenIP extracts the bind IP from a listen address such as "[::]",
// "0.0.0.0", "[::]:8080" or "127.0.0.1:80". A wildcard bind yields no /24 to
// trust (nil).
func parseListenIP(addr string) net.IP {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil
	}
	if host, _, err := net.SplitHostPort(addr); err == nil {
		addr = host
	}
	addr = strings.Trim(addr, "[]")
	if addr == "" || addr == "0.0.0.0" || addr == "::" {
		return nil
	}
	return net.ParseIP(addr)
}

// isTrustedPeer reports whether the immediate TCP peer address is in the
// trusted-proxy set, gating whether X-Forwarded-* headers are honored.
func (s *Server) isTrustedPeer(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, n := range s.trustedProxies {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
