//build +linux
package adapter

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"syscall"
	"unsafe"

	"github.com/arcpop/govpn/core"
	"golang.org/x/sys/unix"
)

const (
	IFF_TUN   = 0x0001
	IFF_TAP   = 0x0002
	IFF_NO_PI = 0x1000
)

type tapInterface struct {
	fd, mtu int
	name    string
	macAddr core.MacAddr

	writeChannel, readChannel chan []byte
}

func newTAP(name string, mtu, queueSize int) (Instance, error) {
	fd, err := unix.Open("/dev/net/tun", unix.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("adapter: failed to open /dev/net/tun: " + err.Error())
	}
	var flags uint16 = IFF_TAP | IFF_NO_PI
	var ifr_req [32]byte
	b := syscall.StringByteSlice(name)
	if name != "" && len(b) < 16 {
		copy(ifr_req[0:15], b[:])
	}
	binary.LittleEndian.PutUint16(ifr_req[16:], flags)
	err = unix.IoctlSetWinsize(fd, unix.TUNSETIFF, (*unix.Winsize)(unsafe.Pointer(&ifr_req[0])))
	if err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("adapter: failed to set device to TAP mode: " + err.Error())
	}

	i := &tapInterface{
		name:         parseName(ifr_req[0:16]),
		fd:           fd,
		writeChannel: make(chan []byte, queueSize),
		readChannel:  make(chan []byte, queueSize),
	}
	iface, err := net.InterfaceByName(i.name)
	if err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("adapter: failed find interface: " + err.Error())
	}
	copy(i.macAddr[:], iface.HardwareAddr[:])
	i.mtu = iface.MTU
	go i.toTapWorker()
	go i.fromTapWorker()
	return i, nil
}

func (t *tapInterface) toTapWorker() {
	for p, ok := <-t.writeChannel; ok; p, ok = <-t.writeChannel {
		_, err := unix.Write(t.fd, p)
		if err != nil {
			log.Println("tap: error when writing to tap: " + err.Error())
		}
	}
}
func (t *tapInterface) fromTapWorker() {
	for {
		p := make([]byte, 1800)
		n, err := unix.Read(t.fd, p)
		if err != nil {
			log.Println("tap: error when reading from tap: " + err.Error())
			continue
		}
		p = p[:n]
		t.readChannel <- p
	}
}

func parseName(s []byte) string {
	i := 0
	for ; i < len(s); i++ {
		if s[i] == 0 {
			break
		}
	}
	return string(s[0:i])
}

func (t *tapInterface) Close() error {
	return unix.Close(t.fd)
}

func (t *tapInterface) GetName() string {
	return t.name
}

func (t *tapInterface) ReceiveChannel() <-chan []byte {
	return t.readChannel
}

func (t *tapInterface) TransmitChannel() chan<- []byte {
	return t.writeChannel
}

func (t *tapInterface) GetMTU() int {
	return t.mtu
}

func (t *tapInterface) GetMACAddress() *core.MacAddr {
	var a core.MacAddr
	copy(a[:], t.macAddr[:])
	return &a
}
