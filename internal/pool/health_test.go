package pool

import (
	"testing"
	"time"
)

func TestParseHealthCheckDefaultsUsesCurrentDefaults(t *testing.T) {
	defaults, err := ParseHealthCheckDefaults(nil)
	if err != nil {
		t.Fatalf("parse defaults: %v", err)
	}
	if defaults.Interval != 30*time.Second || defaults.Timeout != 5*time.Second || defaults.MaxFail != 3 {
		t.Fatalf("unexpected defaults: %#v", defaults)
	}
	if defaults.TestURL != "https://www.google.com/generate_204" {
		t.Fatalf("unexpected default test url: %q", defaults.TestURL)
	}
}

func TestParseHealthCheckDefaultsAcceptsConfiguredValues(t *testing.T) {
	defaults, err := ParseHealthCheckDefaults(&HealthCheckConfig{
		Interval: "2m",
		Timeout:  "10s",
		URL:      "https://example.com/health",
		MaxFail:  5,
	})
	if err != nil {
		t.Fatalf("parse configured defaults: %v", err)
	}
	if defaults.Interval != 2*time.Minute || defaults.Timeout != 10*time.Second || defaults.MaxFail != 5 {
		t.Fatalf("unexpected configured defaults: %#v", defaults)
	}
	if defaults.TestURL != "https://example.com/health" {
		t.Fatalf("unexpected test url: %q", defaults.TestURL)
	}
}

func TestClassifyIPType(t *testing.T) {
	cases := []struct {
		name string
		info IPInfo
		want string
	}{
		{
			name: "mobile isp",
			info: IPInfo{ISP: "T-Mobile USA"},
			want: "mobile",
		},
		{
			name: "datacenter org",
			info: IPInfo{Org: "Amazon.com, Inc."},
			want: "datacenter",
		},
		{
			name: "datacenter isp",
			info: IPInfo{ISP: "The Constant Company, LLC", Domain: "constant.com"},
			want: "datacenter",
		},
		{
			name: "residential broadband",
			info: IPInfo{ISP: "Comcast Cable Communications"},
			want: "residential",
		},
		{
			name: "security flag",
			info: IPInfo{VPN: true, ISP: "Comcast Cable Communications"},
			want: "vpn",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyIPType(tc.info); got != tc.want {
				t.Fatalf("classifyIPType() = %q, want %q", got, tc.want)
			}
		})
	}
}
