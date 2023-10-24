package serverswitch

import "net"

type ServerSwitch interface {
	PowerOn(nodeName string) error
	PowerOff(nodeName string) error
	IsServerOn(nodeName string) error
}

type IpmiServerSwitch struct {
	endpoint net.Addr
}

func NewIpmiServerSwitch(endpoint net.Addr) *IpmiServerSwitch {
	return &IpmiServerSwitch{endpoint: endpoint}
}

func (i *IpmiServerSwitch) PowerOn(nodeName string) error {
	return nil
}

func (i *IpmiServerSwitch) PowerOff(nodeName string) error {
	return nil
}

func (i *IpmiServerSwitch) IsServerOn(nodeName string) error {
	return nil
}
