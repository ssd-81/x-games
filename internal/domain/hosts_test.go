package domain

import "testing"

func TestDetectHost(t *testing.T) {
	cases := []struct {
		org  string
		want Host
	}{
		{"PayPal", HostCompany},
		{"Meta", HostCompany},
		{"UGREEN", HostCompany},
		{"Twitch", HostCompany},
		{"nebius", HostCompany},
		{"Razorpay Software Pvt Ltd", HostCompany},
		{"Acme Technologies", HostCompany},
		{"OpenCV", HostCompany},
		{"Indian Institute of Technology (IIT), Patna", HostUniversity},
		{"Chhattisgarh Institute of Technology, Raipur", HostUniversity},
		{"The NorthCap University (NCU), Gurgaon", HostUniversity},
		{"Masters' Union School of Business", HostUniversity},
		{"Vasavi College of Engineering (VCE), Hyderabad", HostUniversity},
		{"Hack Club", HostCommunity},
		{"Student Organization", HostCommunity},
		{"", HostUnknown},
		{"", HostUnknown},
	}
	for _, tc := range cases {
		if got := DetectHost(tc.org, nil); got != tc.want {
			t.Errorf("DetectHost(%q) = %s, want %s", tc.org, got, tc.want)
		}
	}
}

func TestDetectHostWithUserRegistry(t *testing.T) {
	registry := map[string]bool{"my startup": true}
	if got := DetectHost("My Startup", registry); got != HostCompany {
		t.Errorf("DetectHost with registry = %s, want company", got)
	}
}
