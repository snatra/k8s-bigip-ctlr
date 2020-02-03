package agent

import (
	. "github.com/F5Networks/k8s-bigip-ctlr/pkg/agent/cccl"
	"github.com/F5Networks/k8s-bigip-ctlr/pkg/resource"
	log "github.com/F5Networks/k8s-bigip-ctlr/pkg/vlogger"
)

type agentCCCL struct {
	*CCCLManager
}

func (ag *agentCCCL) Init(params interface{}) error {
	log.Infof("[CCCL] Initializing CCCL Agent")
	ccclParams := params.(*Params)
	ag.CCCLManager = NewCCCLManager(ccclParams)
	ag.SetupL2L3()
	ag.SetupL4L7()
	return nil
}

func (ag *agentCCCL) Deploy(req interface{}) error {
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
	log.Infof("[CCCL] Removing CCCL Partition %v \n", partition)
	return nil
}

func (ag *agentCCCL) DeInit() error {
	log.Infof("[CCCL] DeInitializing CCCL Agent\n")
	ag.NodePoller.Stop()
	ag.ConfigWriter().Stop()
	ag.CleanPythonDriver()
	return nil
}

func (ag *agentCCCL) IsImplInAgent(rsrc string) bool {
	return false
}
