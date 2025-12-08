package utils

import (
	"testing"
)

func TestGetNetworkInfo(t *testing.T) {
	info, err := GetNetworkInfo()
	if err != nil {
		t.Fatalf("GetNetworkInfo failed: %v", err)
	}

	if info.Hostname == "" {
		t.Error("Expected hostname to be non-empty")
	}

	if info.IPv4 == "" {
		t.Error("Expected IPv4 to be non-empty")
	}

	if !ValidateIPv4(info.IPv4) {
		t.Errorf("Expected valid IPv4 address, got: %s", info.IPv4)
	}

	t.Logf("Hostname: %s", info.Hostname)
	t.Logf("IPv4: %s", info.IPv4)
	t.Logf("Interface: %s", info.Interface)
}

func TestGetAllIPv4Addresses(t *testing.T) {
	ips, err := GetAllIPv4Addresses()
	if err != nil {
		t.Fatalf("GetAllIPv4Addresses failed: %v", err)
	}

	if len(ips) == 0 {
		t.Error("Expected at least one IPv4 address")
	}

	for _, ip := range ips {
		if !ValidateIPv4(ip) {
			t.Errorf("Invalid IPv4 address: %s", ip)
		}
		t.Logf("Found IPv4: %s", ip)
	}
}

func TestValidateIPv4(t *testing.T) {
	tests := []struct {
		ip    string
		valid bool
	}{
		{"192.168.1.1", true},
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"255.255.255.255", true},
		{"0.0.0.0", true},
		{"invalid", false},
		{"256.1.1.1", false},
		{"192.168.1", false},
		{"2001:db8::1", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			result := ValidateIPv4(tt.ip)
			if result != tt.valid {
				t.Errorf("ValidateIPv4(%q) = %v, want %v", tt.ip, result, tt.valid)
			}
		})
	}
}

func TestGetFQDN(t *testing.T) {
	fqdn, err := GetFQDN()
	if err != nil {
		t.Fatalf("GetFQDN failed: %v", err)
	}

	if fqdn == "" {
		t.Error("Expected FQDN to be non-empty")
	}

	t.Logf("FQDN: %s", fqdn)
}
