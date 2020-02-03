package agent

import (
	"fmt"
	. "github.com/F5Networks/k8s-bigip-ctlr/pkg/agent/cccl"
	"github.com/F5Networks/k8s-bigip-ctlr/pkg/resource"
)

type agentCCCL struct {
	*CCCLManager
}

func (ag *agentCCCL) Init(params interface{}) error {
	ccclParams := params.(*Params)
	ag.CCCLManager = NewCCCLManager(ccclParams)
	ag.SetupL2L3()
	//ag.SetupL4L7()
	return nil
}

func (ag *agentCCCL) Deploy(req interface{}) error {
	fmt.Println("In Deploy CCCL")
	msgReq := req.(resource.MessageRequest)
	ag.ResourceRequest = msgReq.ResourceRequest
	switch msgReq.MsgType {
	case MsgTypeSendFDB:
		ag.SendFDBEntries()
	case MsgTypeSendARP:
		ag.SendARPEntries()
	case MsgTypeSendDecl:
		ag.OutputConfigLocked()
	}
	return nil
}

func (ag *agentCCCL) Remove(partition string) error {
	fmt.Printf("Removing CCCL Partition %v \n", partition)
	return nil
}

func (ag *agentCCCL) DeInit() error {
	fmt.Printf("DeInit\n")
	ag.NodePoller.Stop()
	ag.ConfigWriter().Stop()
	ag.CleanPythonDriver()
	return nil
}
