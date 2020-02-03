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
		log.Debugf("[AS3] Cannot fetch latest schema %v", err)
		log.Debugf("[AS3] Falling back to local AS3 schema")
	}

	ag.ReqChan = make(chan resource.MessageRequest, 1)
	if ag.ReqChan != nil {
		go ag.ConfigDeployer()
	}

	version, err := ag.AS3Manager.PostManager.GetBigipAS3Version()
	if err != nil {
		log.Errorf("[AS3] App services are not installed on BIGIP")
		return  err
	}

	log.Debugf("[AS3] BIGIP is serving with AS3 version %v", version)

	return nil
}

func (ag *agentAS3) Deploy(req interface{}) error {
	msgReq := req.(resource.MessageRequest)
	select {
	case ag.ReqChan <- msgReq:
	case <-ag.ReqChan:
		ag.ReqChan <- msgReq
	}
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

func (ag *agentAS3) IsImplInAgent(rsrc string) bool {
	if resource.ResourceTypeCfgMap == rsrc{
		return true
	}
	return false
}
