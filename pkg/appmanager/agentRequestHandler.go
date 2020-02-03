package appmanager

import(
	cisAgent "github.com/F5Networks/k8s-bigip-ctlr/pkg/agent"
	. "github.com/F5Networks/k8s-bigip-ctlr/pkg/resource"
)

func (appMgr Manager) deployResource() error {
	// Generate Agent Request
	// TODO this is initial tightly coupled version
	appMgr.deployFDB()
	deployCfg := ResourceRequest{ Resources: appMgr.resources,
		CustomProfiles: appMgr.customProfiles, IrulesMap: appMgr.irulesMap,
		IntDgMap: appMgr.intDgMap, IntF5Res: appMgr.intF5Res}
	agentReq := MessageRequest{MsgType: cisAgent.MsgTypeSendDecl, ResourceRequest: deployCfg}
	// Handle resources to agent and deploy to BIG-IP
	appMgr.AgentCIS.Deploy(agentReq)
	return nil
}

func (appMgr Manager) deployARP() error {
	agentCIS := appMgr.getL2L3Agent()
	deployCfg := ResourceRequest{ Resources: appMgr.resources }//PoolMembers: nil, // TODO This comes from response handler
	// Generate Agent Request
	agentReq := MessageRequest{MsgType: cisAgent.MsgTypeSendARP, ResourceRequest: deployCfg}
	// Handle resources to agent and deploy to BIG-IP
	agentCIS.Deploy(agentReq)
	return nil
}

func (appMgr Manager) deployFDB() error {
	agentCIS := appMgr.getL2L3Agent()
	// Generate Agent Request
	deployCfg := ResourceRequest{ Resources: appMgr.resources }
	agentReq := MessageRequest{MsgType: cisAgent.MsgTypeSendFDB, ResourceRequest: deployCfg}
	// Handle resources to agent and deploy to BIG-IP
	agentCIS.Deploy(agentReq)
	return nil
}

func (appMgr Manager) getL2L3Agent() cisAgent.CISAgentInterface{
	if appMgr.AgentCIS != nil && appMgr.AgentCCCL != nil {
		return appMgr.AgentCCCL
	}
	return appMgr.AgentCIS
}


