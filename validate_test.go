package main

import (
	"testing"
)

func TestValidateIPAddress(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// Valid IPv4 addresses
		{"Valid IP 1", "192.168.1.1", false},
		{"Valid IP 2", "10.0.0.1", false},
		{"Valid IP 3", "172.16.0.1", false},
		{"Valid IP edge case 1", "255.255.255.255", false},
		{"Valid IP edge case 2", "0.0.0.0", false},
		
		// Valid hostnames
		{"Valid hostname simple", "printer", false},
		{"Valid hostname with subdomain", "printer.local", false},
		{"Valid hostname FQDN", "printer.example.com", false},
		{"Valid hostname with hyphen", "my-printer.local", false},
		{"Valid hostname complex", "prusa-mk4-01.home.local", false},
		
		// Invalid cases
		{"Empty string", "", true},
		{"Invalid IP too high", "256.1.1.1", true},
		{"Invalid IP format", "192.168.1", true},
		{"Invalid IP letters", "192.168.1.a", true},
		{"Invalid hostname starts with hyphen", "-printer.local", true},
		{"Invalid hostname ends with hyphen", "printer-.local", true},
		{"Invalid hostname special chars", "printer@home.local", true},
		{"Invalid hostname too long", "a" + string(make([]byte, 260)), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateIPAddress(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateIPAddress(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}
