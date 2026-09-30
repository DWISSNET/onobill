// Package snmpolt — monitoring OLT (Optical Line Terminal) via SNMP untuk FTTH.
// Mendukung baca status ONU/ONT: nama, serial, RX power, status online.
// OID bersifat generik; sesuaikan dengan vendor OLT (ZTE/Huawei/FiberHome).
package snmpolt

import (
	"fmt"
	"time"

	"github.com/gosnmp/gosnmp"
)

// ONU satu pelanggan FTTH di OLT.
type ONU struct {
	Index    int     `json:"index"`
	Name     string  `json:"name"`
	Serial   string  `json:"serial"`
	RxPower  float64 `json:"rx_power_dbm"` // daya terima optik (dBm)
	TxPower  float64 `json:"tx_power_dbm"`
	Online   bool    `json:"online"`
	LastSeen string  `json:"last_seen"`
}

// Client SNMP ke OLT.
type Client struct {
	Host      string
	Community string
	Port      uint16
	Timeout   time.Duration
	snmp      *gosnmp.GoSNMP
}

func NewClient(host, community string) *Client {
	return &Client{
		Host:      host,
		Community: community,
		Port:      161,
		Timeout:   3 * time.Second,
	}
}

func (c *Client) connect() error {
	c.snmp = &gosnmp.GoSNMP{
		Target:    c.Host,
		Port:      c.Port,
		Community: c.Community,
		Version:   gosnmp.Version2c,
		Timeout:   c.Timeout,
		Retries:   1,
	}
	return c.snmp.Connect()
}

// OID generik — sesuaikan per vendor OLT.
// Catatan: OID spesifik ZTE/Huawei/FiberHome berbeda; ini placeholder umum.
var (
	oidSysName   = "1.3.6.1.2.1.1.5.0"   // sysName
	oidSysUpTime = "1.3.6.1.2.1.1.3.0"   // sysUpTime
	oidIfDescr   = "1.3.6.1.2.1.2.2.1.2" // ifDescr (tabel interface/PON)
	oidIfOper    = "1.3.6.1.2.1.2.2.1.8" // ifOperStatus (1=up,2=down)
)

// SysInfo info dasar OLT (nama + uptime). Berguna untuk test koneksi SNMP.
func (c *Client) SysInfo() (name string, uptime time.Duration, err error) {
	if err := c.connect(); err != nil {
		return "", 0, fmt.Errorf("snmp connect: %w", err)
	}
	defer c.snmp.Conn.Close()
	res, err := c.snmp.Get([]string{oidSysName, oidSysUpTime})
	if err != nil {
		return "", 0, fmt.Errorf("snmp get: %w", err)
	}
	for _, v := range res.Variables {
		switch v.Name {
		case oidSysName:
			if b, ok := v.Value.([]byte); ok {
				name = string(b)
			}
		case oidSysUpTime:
			if t, ok := v.Value.(uint32); ok {
				uptime = time.Duration(t) * 10 * time.Millisecond
			}
		}
	}
	return name, uptime, nil
}

// ListInterfaces membaca tabel interface (PON/ONU) beserta status operasional.
// Mengembalikan map ifDescr -> online.
func (c *Client) ListInterfaces() (map[string]bool, error) {
	if err := c.connect(); err != nil {
		return nil, fmt.Errorf("snmp connect: %w", err)
	}
	defer c.snmp.Conn.Close()
	descr := map[int]string{}
	status := map[int]bool{}
	// Walk ifDescr
	err := c.snmp.BulkWalk(oidIfDescr, func(pdu gosnmp.SnmpPDU) error {
		idx := lastIndex(pdu.Name)
		if b, ok := pdu.Value.([]byte); ok {
			descr[idx] = string(b)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk ifdescr: %w", err)
	}
	// Walk ifOperStatus
	err = c.snmp.BulkWalk(oidIfOper, func(pdu gosnmp.SnmpPDU) error {
		idx := lastIndex(pdu.Name)
		if v, ok := pdu.Value.(int); ok {
			status[idx] = (v == 1) // 1 = up
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk ifoper: %w", err)
	}
	out := map[string]bool{}
	for idx, d := range descr {
		out[d] = status[idx]
	}
	return out, nil
}

// lastIndex mengambil angka terakhir dari OID (index tabel SNMP).
func lastIndex(oid string) int {
	n := 0
	mult := 1
	// baca digit dari belakang sampai titik
	i := len(oid) - 1
	for i >= 0 && oid[i] != '.' {
		n += int(oid[i]-'0') * mult
		mult *= 10
		i--
	}
	return n
}
