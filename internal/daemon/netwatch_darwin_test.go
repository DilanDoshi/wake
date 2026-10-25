//go:build darwin

package daemon

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

// routeMessage is a routing message's header as the kernel writes it.
func routeMessage(typ uint8, flags, errno int32) []byte {
	h := unix.RtMsghdr{Msglen: unix.SizeofRtMsghdr, Version: unix.RTM_VERSION, Type: typ, Flags: flags, Errno: errno}
	return append([]byte(nil), unsafe.Slice((*byte)(unsafe.Pointer(&h)), unix.SizeofRtMsghdr)...)
}

// addressMessage is an interface address message, a header shorter than a
// route's, as the kernel writes it.
func addressMessage(typ uint8) []byte {
	h := unix.IfaMsghdr{Msglen: unix.SizeofIfaMsghdr, Version: unix.RTM_VERSION, Type: typ}
	return append([]byte(nil), unsafe.Slice((*byte)(unsafe.Pointer(&h)), unix.SizeofIfaMsghdr)...)
}

// linkMessage is an interface's state, as the kernel writes it.
func linkMessage(flags int32) []byte {
	h := unix.IfMsghdr{Msglen: unix.SizeofIfMsghdr, Version: unix.RTM_VERSION, Type: unix.RTM_IFINFO, Flags: flags}
	return append([]byte(nil), unsafe.Slice((*byte)(unsafe.Pointer(&h)), unix.SizeofIfMsghdr)...)
}

// Only an address gained, an interface up with a link, or a gateway route added
// or changed reads as the network coming back. The host routes and lookup
// misses a connected machine sends all the time do not, nor a link going down,
// the loopback, a failed request's echo, or a message of another version or cut
// short.
func TestOnlyAnAddressALinkOrAGatewayRouteReadsAsTheNetworkBack(t *testing.T) {
	gateway := int32(unix.RTF_UP | unix.RTF_GATEWAY)
	wrongVersion := routeMessage(unix.RTM_ADD, gateway, 0)
	wrongVersion[2] = unix.RTM_VERSION + 1
	for _, tc := range []struct {
		name string
		msg  []byte
		want bool
	}{
		{"an address gained", addressMessage(unix.RTM_NEWADDR), true},
		{"an interface up with a link", linkMessage(unix.IFF_UP | unix.IFF_RUNNING | unix.IFF_BROADCAST), true},
		{"an interface up with no link", linkMessage(unix.IFF_UP), false},
		{"an interface down", linkMessage(0), false},
		{"the loopback up", linkMessage(unix.IFF_UP | unix.IFF_RUNNING | unix.IFF_LOOPBACK), false},
		{"a gateway route added", routeMessage(unix.RTM_ADD, gateway, 0), true},
		{"a gateway route changed", routeMessage(unix.RTM_CHANGE, gateway, 0), true},
		{"a host route via a gateway", routeMessage(unix.RTM_ADD, gateway|unix.RTF_HOST, 0), false},
		{"a route with no gateway", routeMessage(unix.RTM_ADD, unix.RTF_UP, 0), false},
		{"a refused add", routeMessage(unix.RTM_ADD, gateway, int32(unix.EEXIST)), false},
		{"a route deleted", routeMessage(unix.RTM_DELETE, gateway, 0), false},
		{"a lookup miss", routeMessage(unix.RTM_MISS, 0, 0), false},
		{"an address lost", addressMessage(unix.RTM_DELADDR), false},
		{"another version", wrongVersion, false},
		{"a route message cut short", routeMessage(unix.RTM_ADD, gateway, 0)[:unix.SizeofIfaMsghdr], false},
		{"nothing", nil, false},
	} {
		if got := routeChanged(tc.msg); got != tc.want {
			t.Errorf("%s: routeChanged = %v, want %v", tc.name, got, tc.want)
		}
	}
}
