package monitor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"
)

var errPrivateAddress = errors.New("address is not public")

func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	// Exclude shared, documentation, benchmark, reserved and IPv6 transition ranges.
	for _, prefix := range []string{
		"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24",
		"198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4",
		"2001::/23", "2001:db8::/32", "2002::/16",
	} {
		if netip.MustParsePrefix(prefix).Contains(ip) {
			return false
		}
	}
	return ip.Is4() || netip.MustParsePrefix("2000::/3").Contains(ip)
}

type publicDialer struct {
	lookup func(context.Context, string, string) ([]netip.Addr, error)
	dial   func(context.Context, string, string) (net.Conn, error)
}

func dialPublic(ctx context.Context, network, address string) (net.Conn, error) {
	// The HTTP transport can detach connection attempts from the requesting context.
	return dialPublicWithin(ctx, network, address, faviconFetchTimeout)
}

func dialPublicWithin(ctx context.Context, network, address string, timeout time.Duration) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var dialer net.Dialer
	return (publicDialer{
		lookup: net.DefaultResolver.LookupNetIP,
		dial:   dialer.DialContext,
	}).dialContext(ctx, network, address)
}

func (d publicDialer) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("public address: %w", err)
	}
	ips, err := d.lookup(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("public DNS: %w", err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("public host has no addresses")
	}
	for _, ip := range ips {
		if !publicIP(ip) {
			return nil, errPrivateAddress
		}
	}
	var lastErr error
	for i, ip := range ips {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		attempt := ctx
		cancel := func() {}
		if deadline, ok := ctx.Deadline(); ok {
			// Reserve a share of the remaining deadline so a stalled IP cannot starve fallback addresses.
			attempt, cancel = context.WithTimeout(ctx, time.Until(deadline)/time.Duration(len(ips)-i))
		}
		// Dial only the checked IPs to prevent DNS rebinding between validation and connection.
		conn, err := d.dial(attempt, network, net.JoinHostPort(ip.String(), port))
		cancel()
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("public connection: %w", lastErr)
}
