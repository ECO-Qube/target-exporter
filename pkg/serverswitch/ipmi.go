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
	GetBmcEndpoint() string
}

type IpmiServerSwitch struct {
	bmcEndpoint string
	c           *Client
	connection  *Connection
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
	client, err := open(*c)
	if err != nil {
		return nil, err
	}

	return &IpmiServerSwitch{bmcEndpoint: endpoint, c: client, connection: c, logger: logger}, nil
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
	request := &Request{
		NetworkFunction: NetworkFunctionChassis,
		Command:         CommandChassisControl,
		Data:            &ChassisControlRequest{ChassisControl: ControlPowerCycle},
	}
	response := &ChassisControlResponse{}
	err := i.c.Send(request, response)
	if err != nil {
		i.logger.Error("error sending power off command", zap.Error(err))
		return err
	}
	if response.CompletionCode != CommandCompleted {
		i.logger.Error("error powering off server", zap.Uint8("completion_code", uint8(response.CompletionCode)))
		return fmt.Errorf("error powering off server")
	}
	return nil
}

func (i *IpmiServerSwitch) IsServerOn() (bool, error) {
	request := &Request{
		NetworkFunction: NetworkFunctionChassis,
		Command:         CommandChassisStatus,
		Data:            &ChassisStatusRequest{},
	}
	response := &ChassisStatusResponse{}
	if i.c == nil {
		return false, fmt.Errorf("client is nil", zap.String("server", i.bmcEndpoint))
	}
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

func (i *IpmiServerSwitch) GetBmcEndpoint() string {
	return i.bmcEndpoint
}

// RetryConn will reopen a client connection if it is closed. It will close an existing connection if present.
func (i *IpmiServerSwitch) RetryConn() error {
	_ = i.c.Close()
	client, err := open(*i.connection)
	if err != nil {
		return err
	}
	i.c = client
	return nil
}

func open(c Connection) (*Client, error) {
	client, err := NewClient(&c)
	if err != nil {
		return nil, err
	}
	err = client.Open()
	if err != nil {
		return nil, err
	}
	return client, nil
}
