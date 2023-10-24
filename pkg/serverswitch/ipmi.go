package serverswitch

import (
	"fmt"
	. "github.com/vmware/goipmi"
	"go.uber.org/zap"
)

type ServerSwitch interface {
	PowerOn() error
	PowerOff() error
	IsServerOn() (bool, error)
}

type IpmiServerSwitch struct {
	bmcEndpoint string
	c           *Client
	logger      *zap.Logger
}

func NewIpmiServerSwitch(endpoint, username, password string, logger *zap.Logger) (*IpmiServerSwitch, error) {
	c := &Connection{
		Hostname:  endpoint,
		Port:      623,
		Username:  username,
		Password:  password,
		Interface: "lan",
	}
	client, err := NewClient(c)
	if err != nil {
		return nil, err
	}
	err = client.Open()
	if err != nil {
		return nil, err
	}

	return &IpmiServerSwitch{bmcEndpoint: endpoint, c: client, logger: logger}, nil
}

func (i *IpmiServerSwitch) PowerOn() error {
	//err := i.c.Open()
	//if err != nil {
	//	return err
	//}
	//defer i.c.Close()
	return nil
}

func (i *IpmiServerSwitch) PowerOff() error {
	return nil
}

func (i *IpmiServerSwitch) IsServerOn() (bool, error) {
	request := &Request{
		NetworkFunction: NetworkFunctionChassis,
		Command:         CommandChassisStatus,
		Data:            &ChassisStatusRequest{},
	}
	response := &ChassisStatusResponse{}
	err := i.c.Send(request, response)
	if err != nil {
		return false, err
	}
	if response.CompletionCode != CommandCompleted {
		i.logger.Error("error getting chassis status", zap.Uint8("completion_code", uint8(response.CompletionCode)))
		return false, fmt.Errorf("error getting chassis status")
	}
	return response.IsSystemPowerOn(), nil
}
