package fins

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"time"
)

// UdpClient Omron FINS client
type UdpClient struct {
	conn              *net.UDPConn
	dst               finsAddress
	src               finsAddress
	sid               byte
	closed            bool
	responseTimeoutMs time.Duration
	byteOrder         binary.ByteOrder
}

// NewClient creates a new Omron FINS client

func NewUDPConn(remoteAddr, remotePort, localAddr, localPort string, plcAddrNetwork, plcAddrNode, plcAddrUnit, localAddrNetwork, localAddrNode, localAddrUnit byte) (Client, error) {
	c := new(UdpClient)
	// 与 TCP 客户端保持一致：dst 是本地地址，src 是 PLC 地址
	c.dst = finsAddress{
		network: localAddrNetwork,
		node:    localAddrNode,
		unit:    localAddrUnit,
	}
	c.src = finsAddress{
		network: plcAddrNetwork,
		node:    plcAddrNode,
		unit:    plcAddrUnit,
	}
	c.responseTimeoutMs = DEFAULT_RESPONSE_TIMEOUT
	c.byteOrder = binary.BigEndian

	// raddr, err := net.ResolveUDPAddr("udp", remoteAddr+":"+remoteAddr)
	// if err != nil {
	// 	return nil, err
	// }
	raddr := &net.UDPAddr{
		IP:   net.ParseIP(remoteAddr),
		Port: 5010, // FINS UDP 默认端口
	}

	// 如果用户指定了端口，使用用户指定的端口
	if remotePort != "" {
		if port, err := strconv.Atoi(remotePort); err == nil {
			raddr.Port = port
		}
	}
	var err error

	var laddr *net.UDPAddr
	if localAddr == "" && localPort == "" {
		laddr = nil
	} else {
		// 修复本地地址解析
		laddr, err = net.ResolveUDPAddr("udp", localAddr+":"+localPort)
		if err != nil {
			return nil, err
		}
	}

	conn, err := net.DialUDP("udp", laddr, raddr)
	if err != nil {
		return nil, err
	}

	c.conn = conn

	// 移除 goroutine，改为单线程实现
	// c.resp = make([]chan response, 256) // storage for all responses, sid is byte - only 256 values
	// go c.listenLoop()
	return c, nil
}

// Set byte order
// Default value: binary.BigEndian
func (c *UdpClient) SetByteOrder(o binary.ByteOrder) {
	c.byteOrder = o
}

// Set response timeout duration (ms).
// Default value: 20ms.
// A timeout of zero can be used to block indefinitely.
func (c *UdpClient) SetTimeoutMs(t uint) {
	c.responseTimeoutMs = time.Duration(t) * time.Millisecond
}

// Close Closes an Omron FINS connection
func (c *UdpClient) Close() {
	c.closed = true
	c.conn.Close()
}

// ReadWordsToUint16 读取plc连续数据(uint16)地址区域
func (c *UdpClient) ReadWordsToUint16(memoryArea byte, address uint16, readCount uint16) ([]uint16, error) {
	if checkIsWordMemoryArea(memoryArea) == false {
		return nil, IncompatibleMemoryAreaError{memoryArea}
	}
	command := readCommand(memAddr(memoryArea, address), readCount)
	r, e := c.sendCommand(command)
	e = checkResponse(r, e)
	if e != nil {
		return nil, e
	}

	data := make([]uint16, readCount, readCount)
	for i := 0; i < int(readCount); i++ {
		data[i] = c.byteOrder.Uint16(r.data[i*2 : i*2+2])
	}

	return data, nil
}

// ReadWordsToUint32 读取plc连续数据(uint32)地址区域
func (c *UdpClient) ReadWordsToUint32(memoryArea byte, address uint16, readCount uint16) ([]uint32, error) {
	if checkIsWordMemoryArea(memoryArea) == false {
		return nil, IncompatibleMemoryAreaError{memoryArea}
	}
	command := readCommand(memAddr(memoryArea, address), readCount)
	r, e := c.sendCommand(command)
	e = checkResponse(r, e)
	if e != nil {
		return nil, e
	}

	data := make([]uint32, readCount, readCount)
	for i := 0; i < int(readCount); i++ {
		data[i] = c.byteOrder.Uint32(r.data[i*4 : i*4+4])
	}

	return data, nil
}

// ReadWordsToUint32 读取plc连续数据(uint32)地址区域
func (c *UdpClient) ReadBytes(memoryArea byte, address uint16, readCount uint16) ([]byte, error) {
	if checkIsWordMemoryArea(memoryArea) == false {
		return nil, IncompatibleMemoryAreaError{memoryArea}
	}
	command := readCommand(memAddr(memoryArea, address), readCount)
	r, e := c.sendCommand(command)
	e = checkResponse(r, e)
	if e != nil {
		return nil, e
	}

	return r.data, nil
}

// ReadString 读取plc连续数据(string)地址区域
func (c *UdpClient) ReadString(memoryArea byte, address uint16, readCount uint16) (string, error) {
	data, e := c.ReadBytes(memoryArea, address, readCount)
	if e != nil {
		return "", e
	}
	n := bytes.IndexByte(data, 0)
	if n == -1 {
		n = len(data)
	}
	return string(data[:n]), nil
}

// ReadBits 读取plc连续数据(bool)地址区域
func (c *UdpClient) ReadBits(memoryArea byte, address uint16, bitOffset byte, readCount uint16) ([]bool, error) {
	if checkIsBitMemoryArea(memoryArea) == false {
		return nil, IncompatibleMemoryAreaError{memoryArea}
	}
	command := readCommand(memAddrWithBitOffset(memoryArea, address, bitOffset), readCount)
	r, e := c.sendCommand(command)
	e = checkResponse(r, e)
	if e != nil {
		return nil, e
	}

	data := make([]bool, readCount, readCount)
	for i := 0; i < int(readCount); i++ {
		data[i] = r.data[i]&0x01 > 0
	}

	return data, nil
}

// ReadClock Reads the PLC clock
func (c *UdpClient) ReadClock() (*time.Time, error) {
	r, e := c.sendCommand(clockReadCommand())
	e = checkResponse(r, e)
	if e != nil {
		return nil, e
	}
	year, _ := decodeBCD(r.data[0:1])
	if year < 50 {
		year += 2000
	} else {
		year += 1900
	}
	month, _ := decodeBCD(r.data[1:2])
	day, _ := decodeBCD(r.data[2:3])
	hour, _ := decodeBCD(r.data[3:4])
	minute, _ := decodeBCD(r.data[4:5])
	second, _ := decodeBCD(r.data[5:6])

	t := time.Date(
		int(year), time.Month(month), int(day), int(hour), int(minute), int(second),
		0, // nanosecond
		time.Local,
	)
	return &t, nil
}

// WriteWords Writes words to the PLC data area
func (c *UdpClient) WriteWords(memoryArea byte, address uint16, data []uint16) error {
	if checkIsWordMemoryArea(memoryArea) == false {
		return IncompatibleMemoryAreaError{memoryArea}
	}
	l := uint16(len(data))
	bts := make([]byte, 2*l, 2*l)
	for i := 0; i < int(l); i++ {
		c.byteOrder.PutUint16(bts[i*2:i*2+2], data[i])
	}
	command := writeCommand(memAddr(memoryArea, address), l, bts)

	return checkResponse(c.sendCommand(command))
}

// WriteString Writes a string to the PLC data area
func (c *UdpClient) WriteString(memoryArea byte, address uint16, s string) error {
	if checkIsWordMemoryArea(memoryArea) == false {
		return IncompatibleMemoryAreaError{memoryArea}
	}
	bts := make([]byte, 2*len(s), 2*len(s))
	copy(bts, s)

	command := writeCommand(memAddr(memoryArea, address), uint16((len(s)+1)/2), bts) // TODO: test on real PLC

	return checkResponse(c.sendCommand(command))
}

// WriteBytes Writes bytes array to the PLC data area
func (c *UdpClient) WriteBytes(memoryArea byte, address uint16, b []byte) error {
	if checkIsWordMemoryArea(memoryArea) == false {
		return IncompatibleMemoryAreaError{memoryArea}
	}
	command := writeCommand(memAddr(memoryArea, address), uint16(len(b)), b)
	return checkResponse(c.sendCommand(command))
}

// WriteBits Writes bits to the PLC data area
func (c *UdpClient) WriteBits(memoryArea byte, address uint16, bitOffset byte, data []bool) error {
	if checkIsBitMemoryArea(memoryArea) == false {
		return IncompatibleMemoryAreaError{memoryArea}
	}
	l := uint16(len(data))
	bts := make([]byte, 0, l)
	var d byte
	for i := 0; i < int(l); i++ {
		if data[i] {
			d = 0x01
		} else {
			d = 0x00
		}
		bts = append(bts, d)
	}
	command := writeCommand(memAddrWithBitOffset(memoryArea, address, bitOffset), l, bts)

	return checkResponse(c.sendCommand(command))
}

// SetBit Sets a bit in the PLC data area
func (c *UdpClient) SetBit(memoryArea byte, address uint16, bitOffset byte) error {
	return c.bitTwiddle(memoryArea, address, bitOffset, 0x01)
}

// ResetBit Resets a bit in the PLC data area
func (c *UdpClient) ResetBit(memoryArea byte, address uint16, bitOffset byte) error {
	return c.bitTwiddle(memoryArea, address, bitOffset, 0x00)
}

// ToggleBit Toggles a bit in the PLC data area
func (c *UdpClient) ToggleBit(memoryArea byte, address uint16, bitOffset byte) error {
	b, e := c.ReadBits(memoryArea, address, bitOffset, 1)
	if e != nil {
		return e
	}
	var t byte
	if b[0] {
		t = 0x00
	} else {
		t = 0x01
	}
	return c.bitTwiddle(memoryArea, address, bitOffset, t)
}

func (c *UdpClient) bitTwiddle(memoryArea byte, address uint16, bitOffset byte, value byte) error {
	if checkIsBitMemoryArea(memoryArea) == false {
		return IncompatibleMemoryAreaError{memoryArea}
	}
	mem := memoryAddress{memoryArea, address, bitOffset}
	command := writeCommand(mem, 1, []byte{value})

	return checkResponse(c.sendCommand(command))
}

func (c *UdpClient) nextHeader() *Header {
	sid := c.incrementSid()
	header := defaultCommandHeader(c.src, c.dst, sid)
	return &header
}

func (c *UdpClient) incrementSid() byte {
	c.sid++
	return c.sid
}

func (c *UdpClient) sendCommand(command []byte) (*response, error) {
	header := c.nextHeader()
	bts := c.encodeHeader(*header)
	bts = append(bts, command...)

	_, err := (*c.conn).Write(bts)
	if err != nil {
		return nil, err
	}

	// 单线程实现：直接读取响应，而不是等待通道
	return c.readResponse()
}

// readResponse 单线程读取响应
func (c *UdpClient) readResponse() (*response, error) {
	// 设置读取超时
	if c.responseTimeoutMs > 0 {
		err := c.conn.SetReadDeadline(time.Now().Add(c.responseTimeoutMs * time.Millisecond))
		if err != nil {
			return nil, err
		}
		defer c.conn.SetReadDeadline(time.Time{}) // 清除超时设置
	}

	buf := make([]byte, 2048)
	n, err := bufio.NewReader(c.conn).Read(buf)
	if err != nil {
		return nil, err
	}

	if n > 0 {
		ans := decodeResponse(buf[:n])
		return &ans, nil
	} else {
		return nil, fmt.Errorf("cannot read response: %v", buf)
	}
}

// todo
func (c *UdpClient) encodeHeader(h Header) []byte {
	var icf byte
	icf = 1 << icfBridgesBit
	if h.responseRequired == false {
		icf |= 1 << icfResponseRequiredBit
	}
	if h.messageType == MessageTypeResponse {
		icf |= 1 << icfMessageTypeBit
	}
	bytes := []byte{
		icf, 0x00, h.gatewayCount,
		h.dst.network, h.dst.node, h.dst.unit,
		h.src.network, h.src.node, h.src.unit,
		h.serviceID,
	}
	return bytes
}
