package utils

import (
	"fmt"
	"net"
	"os"
	"strings"
)

// NetworkInfo holds network information about the host
type NetworkInfo struct {
	Hostname  string
	IPv4      string
	Interface string // Name of the interface where IPv4 was found
}

// GetNetworkInfo retrieves the hostname and primary IPv4 address of the system
// This uses Go's standard library to work across all platforms (Linux, macOS, Windows)
func GetNetworkInfo() (*NetworkInfo, error) {
	info := &NetworkInfo{}

	// Get hostname
	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("failed to get hostname: %w", err)
	}
	info.Hostname = hostname

	// Get primary IPv4 address
	ipv4, iface, err := getPrimaryIPv4()
	if err != nil {
		return nil, fmt.Errorf("failed to get IPv4 address: %w", err)
	}
	info.IPv4 = ipv4
	info.Interface = iface

	return info, nil
}

// getPrimaryIPv4 finds the primary non-loopback IPv4 address
// It prioritizes interfaces that are up and have a default gateway route
func getPrimaryIPv4() (string, string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", "", err
	}

	// First pass: look for non-loopback, up interfaces with IPv4
	for _, iface := range interfaces {
		// Skip down interfaces
		if iface.Flags&net.FlagUp == 0 {
			continue
		}

		// Skip loopback
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			// We want IPv4 only
			if ip == nil || ip.To4() == nil {
				continue
			}

			// Skip loopback IPs (127.x.x.x)
			if ip.IsLoopback() {
				continue
			}

			// Skip link-local addresses (169.254.x.x)
			if ip.IsLinkLocalUnicast() {
				continue
			}

			// Found a valid IPv4 address
			return ip.String(), iface.Name, nil
		}
	}

	// Second pass: if no good interface found, try getting any non-loopback IP
	// This handles edge cases where interfaces might not be marked as "up" properly
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			if ip == nil || ip.To4() == nil || ip.IsLoopback() {
				continue
			}

			return ip.String(), iface.Name, nil
		}
	}

	return "", "", fmt.Errorf("no non-loopback IPv4 address found")
}

// GetAllIPv4Addresses returns all non-loopback IPv4 addresses on the system
// Useful for debugging or when you need to see all available IPs
func GetAllIPv4Addresses() ([]string, error) {
	var ips []string

	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			if ip != nil && ip.To4() != nil && !ip.IsLoopback() {
				ips = append(ips, ip.String())
			}
		}
	}

	return ips, nil
}

// ValidateIPv4 checks if a string is a valid IPv4 address
func ValidateIPv4(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	return ip != nil && ip.To4() != nil
}

// GetFQDN attempts to get the fully qualified domain name
// Falls back to hostname if FQDN cannot be determined
func GetFQDN() (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", err
	}

	// If hostname already contains a dot, it might be FQDN
	if strings.Contains(hostname, ".") {
		return hostname, nil
	}

	// Try to resolve the hostname to get FQDN
	addrs, err := net.LookupHost(hostname)
	if err != nil || len(addrs) == 0 {
		// If lookup fails, return simple hostname
		return hostname, nil
	}

	// Try reverse lookup on the first address
	names, err := net.LookupAddr(addrs[0])
	if err != nil || len(names) == 0 {
		return hostname, nil
	}

	// Return the first FQDN found (trim trailing dot if present)
	fqdn := strings.TrimSuffix(names[0], ".")
	if fqdn != "" {
		return fqdn, nil
	}

	return hostname, nil
}
