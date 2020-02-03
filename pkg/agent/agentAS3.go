package agent

import (
	. "github.com/F5Networks/k8s-bigip-ctlr/pkg/agent/as3"
	"github.com/F5Networks/k8s-bigip-ctlr/pkg/resource"
	log "github.com/F5Networks/k8s-bigip-ctlr/pkg/vlogger"
)

type agentAS3 struct {
	*AS3Manager
}

func (ag *agentAS3) Init(params interface{}) error {
	log.Info("[AS3] Initilizing AS3 Agent")
	as3Params := params.(*Params)
	ag.AS3Manager = NewAS3Manager(as3Params)
	err := ag.FetchAS3Schema()
	if err != nil {
		return err
	}
	ag.ReqChan = make(chan resource.MessageRequest, 1)
	if ag.ReqChan != nil {
		go ag.ConfigDeployer()
	}
	return nil
}

func (ag *agentAS3) Deploy(req interface{}) error {
	log.Debug("[AS3] Deploying resource on AS3 Agent")
	msgReq := req.(resource.MessageRequest)

	select {
	case ag.ReqChan <- msgReq:
	case <-ag.ReqChan:
		ag.ReqChan <- msgReq
	}
	log.Debug("[AS3] AS3Manager Accepted the configuration")

	return nil
}

func (ag *agentAS3) Remove(partition string) error {
	log.Debugf("[AS3] Removing AS3 Partition %v", partition)
	ag.DeleteAS3Partition(partition + "_AS3")
	return nil
}

func (ag *agentAS3) DeInit() error {
	log.Debug("[AS3] DeInitializingAgent Request and Rsp channels")
	close(ag.RspChan)
	close(ag.ReqChan)
	return nil
}
