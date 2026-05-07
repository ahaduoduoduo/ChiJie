package pool

import "testing"

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
