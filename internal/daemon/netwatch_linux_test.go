//go:build linux

package daemon

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

// netlinkMessage is one netlink message carrying body, as the kernel writes it.
func netlinkMessage(typ uint16, body []byte) []byte {
	h := unix.NlMsghdr{Len: uint32(unix.SizeofNlMsghdr + len(body)), Type: typ}
	msg := append([]byte(nil), unsafe.Slice((*byte)(unsafe.Pointer(&h)), unix.SizeofNlMsghdr)...)
	msg = append(msg, body...)
	for len(msg)%unix.NLMSG_ALIGNTO != 0 {
		msg = append(msg, 0)
	}
	return msg
}

// routeBody is a route message's header for a destination prefix and table.
func routeBody(dstLen, table uint8) []byte {
	rt := unix.RtMsg{Family: unix.AF_INET, Dst_len: dstLen, Table: table}
	return append([]byte(nil), unsafe.Slice((*byte)(unsafe.Pointer(&rt)), unix.SizeofRtMsg)...)
}

// linkBody is an interface's state for a link message.
func linkBody(flags uint32) []byte {
	ifi := unix.IfInfomsg{Family: unix.AF_UNSPEC, Flags: flags}
	return append([]byte(nil), unsafe.Slice((*byte)(unsafe.Pointer(&ifi)), unix.SizeofIfInfomsg)...)
}

// Only an address gained, an interface up with a link, or a default route added
// to the main table reads as the network coming back - in a read that holds
// others before it too.
func TestOnlyAnAddressALinkOrADefaultRouteReadsAsTheNetworkBack(t *testing.T) {
	neighbour := netlinkMessage(unix.RTM_NEWROUTE, routeBody(24, unix.RT_TABLE_MAIN))
	for _, tc := range []struct {
		name string
		buf  []byte
		want bool
	}{
		{"an address gained", netlinkMessage(unix.RTM_NEWADDR, make([]byte, unix.SizeofIfAddrmsg)), true},
		{"an interface up with a link", netlinkMessage(unix.RTM_NEWLINK, linkBody(unix.IFF_UP|unix.IFF_RUNNING|unix.IFF_BROADCAST)), true},
		{"an interface up with no link", netlinkMessage(unix.RTM_NEWLINK, linkBody(unix.IFF_UP)), false},
		{"the loopback up", netlinkMessage(unix.RTM_NEWLINK, linkBody(unix.IFF_UP|unix.IFF_RUNNING|unix.IFF_LOOPBACK)), false},
		{"a link message cut short", netlinkMessage(unix.RTM_NEWLINK, nil), false},
		{"a default route added", netlinkMessage(unix.RTM_NEWROUTE, routeBody(0, unix.RT_TABLE_MAIN)), true},
		{"a default route behind another route", append(append([]byte(nil), neighbour...), netlinkMessage(unix.RTM_NEWROUTE, routeBody(0, unix.RT_TABLE_MAIN))...), true},
		{"a route to a network", neighbour, false},
		{"a default route in the local table", netlinkMessage(unix.RTM_NEWROUTE, routeBody(0, unix.RT_TABLE_LOCAL)), false},
		{"a route deleted", netlinkMessage(unix.RTM_DELROUTE, routeBody(0, unix.RT_TABLE_MAIN)), false},
		{"an address lost", netlinkMessage(unix.RTM_DELADDR, make([]byte, unix.SizeofIfAddrmsg)), false},
		{"a route message cut short", netlinkMessage(unix.RTM_NEWROUTE, nil), false},
		{"a default route behind one cut short", append(netlinkMessage(unix.RTM_NEWROUTE, nil), netlinkMessage(unix.RTM_NEWROUTE, routeBody(0, unix.RT_TABLE_MAIN))...), true},
		{"nothing", nil, false},
	} {
		if got := routeChanged(tc.buf); got != tc.want {
			t.Errorf("%s: routeChanged = %v, want %v", tc.name, got, tc.want)
		}
	}
}
