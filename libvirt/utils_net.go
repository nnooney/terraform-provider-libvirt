package libvirt

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"os"
	"path"
	"time"
)

const (
	maxIfaceNum = 100
)

// randomMACAddress returns a randomized MAC address
// with libvirt prefix
func randomMACAddress() (string, error) {
	buf := make([]byte, 3)
	rand.Seed(time.Now().UnixNano())
	_, err := rand.Read(buf)
	if err != nil {
		return "", err
	}

	// set local bit and unicast
	buf[0] = (buf[0] | 2) & 0xfe
	// Set the local bit
	buf[0] |= 2

	// avoid libvirt-reserved addresses
	if buf[0] == 0xfe {
		buf[0] = 0xee
	}

	return fmt.Sprintf("52:54:00:%02x:%02x:%02x",
		buf[0], buf[1], buf[2]), nil
}

// randomPort returns a random port
func randomPort() int {
	const minPort = 1024
	const maxPort = 65535

	rand.Seed(time.Now().UnixNano())
	return rand.Intn(maxPort-minPort) + minPort
}

func getNetMaskWithMax16Bits(m net.IPMask) net.IPMask {
	ones, bits := m.Size()

	if bits-ones > 16 {
		if bits == 128 {
			// IPv6 Mask with max 16 bits
			return net.CIDRMask(128-16, 128)
		}

		// IPv4 Mask with max 16 bits
		return net.CIDRMask(32-16, 32)
	}

	return m
}

// networkRange calculates the first and last IP addresses in an IPNet
func networkRange(network *net.IPNet) (net.IP, net.IP) {
	netIP := network.IP.To4()
	lastIP := net.IPv4zero.To4()
	if netIP == nil {
		netIP = network.IP.To16()
		lastIP = net.IPv6zero.To16()
	}
	firstIP := netIP.Mask(network.Mask)
	// intermediate network mask with max 16 bits for hosts
	// We need a mask with max 16 bits since libvirt only supports 65535) IP's per subnet
	// 2^16 = 65536 (minus broadcast and .1)
	intMask := getNetMaskWithMax16Bits(network.Mask)

	for i := 0; i < len(lastIP); i++ {
		lastIP[i] = netIP[i] | ^intMask[i]
	}
	return firstIP, lastIP
}

// a HTTP server that serves files in a directory, used mostly for testing
type fileWebServer struct {
	Dir  string
	Port int
	URL  string

	server *http.Server
}

func (fws *fileWebServer) Start() error {
	dir, err := ioutil.TempDir(fws.Dir, "")
	if err != nil {
		return err
	}

	fws.Dir = dir
	fws.Port = randomPort()
	fws.URL = fmt.Sprintf("http://127.0.0.1:%d", fws.Port)

	handler := http.NewServeMux()
	handler.Handle("/", http.FileServer(http.Dir(dir)))
	fws.server = &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", fws.Port), Handler: handler}
	ln, err := net.Listen("tcp", fws.server.Addr)
	if err != nil {
		return err
	}
	go fws.server.Serve(ln)
	return nil
}

// Adds a file (with some content) in the directory served by the fileWebServer
func (fws *fileWebServer) AddContent(content []byte) (string, *os.File, error) {
	tmpfile, err := ioutil.TempFile(fws.Dir, "file-")
	if err != nil {
		return "", nil, err
	}

	if len(content) > 0 {
		if _, err := tmpfile.Write(content); err != nil {
			return "", nil, err
		}
	}

	return fmt.Sprintf("%s/%s", fws.URL, path.Base(tmpfile.Name())), tmpfile, nil
}

// Symlinks a file into the directory server by the webserver
func (fws *fileWebServer) AddFile(filePath string) (string, error) {
	err := os.Symlink(filePath, path.Join(fws.Dir, path.Base(filePath)))
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s/%s", fws.URL, path.Base(filePath)), nil
}

func (fws *fileWebServer) Stop() {
	os.RemoveAll(fws.Dir)
}
