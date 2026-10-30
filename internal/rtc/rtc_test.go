package rtc

import (
	"net"
	"testing"
)

func TestUsable(t *testing.T) {
	cases := []struct {
		ip   string
		keep bool
	}{
		{ip: "192.168.1.132", keep: true},
		{ip: "2a0a:ef40:1494:7e01::1", keep: true},
		{ip: "169.254.10.1", keep: false},
		{ip: "fe80::1", keep: false},
		{ip: "ff02::1", keep: false},
		{ip: "ff01::1", keep: false},
	}

	for _, c := range cases {
		t.Run(c.ip, func(t *testing.T) {
			if got := usable(net.ParseIP(c.ip)); got != c.keep {
				t.Fatalf("got %v, want %v", got, c.keep)
			}
		})
	}
}

func TestUsableInterface(t *testing.T) {
	cases := []struct {
		name string
		keep bool
	}{
		{name: "en0", keep: true},
		{name: "en1", keep: true},
		{name: "eth0", keep: true},
		{name: "wlan0", keep: true},
		{name: "bridge100", keep: false},
		{name: "utun4", keep: false},
		{name: "vmnet1", keep: false},
		{name: "awdl0", keep: false},
		{name: "llw0", keep: false},
		{name: "ap1", keep: false},
		{name: "anpi0", keep: false},
		{name: "gif0", keep: false},
		{name: "stf0", keep: false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := usableInterface(c.name); got != c.keep {
				t.Fatalf("got %v, want %v", got, c.keep)
			}
		})
	}
}
