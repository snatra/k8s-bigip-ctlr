package agent

import (
	"errors"
)

const (
    MsgTypeSendFDB      = "FDB"
    MsgTypeSendARP      = "ARP"
    MsgTypeSendDecl     = "L4L7Decleration"
)

type CISAgentInterface interface {
    Initializer
    Deployer
    Remover
	DeInitializer
}

// Initializer is the interface that wraps basic Init method.
type Initializer interface {
   //Init(params *Params)(error)
	Init(interface{})(error)
}

// Deployer is the interface that wraps basic Deploy method
type Deployer interface {
   Deploy(req interface{})(error)
}

// Remover is the interface that wraps basic Remove method
type Remover interface {
   Remove(partition string)(error)
}

// De-Initializer is the interface that wraps basic Init method.
type DeInitializer interface {
	//Init(params *Params)(error)
	DeInit()(error)
}

const (
	AS3Agent = "as3"
	CCCLAgent = "cccl"
	//BIGIQ
	//FAST
)

func CreateAgent(agentType string) (CISAgentInterface, error) {
	switch agentType {
	case AS3Agent:
		return new(agentAS3), nil
	case CCCLAgent:
		return new(agentCCCL), nil
	//case BIGIQ:
	//	return new(agentBIGIQ), nil
	//case FAST:
	//	return new(agentFAST), nil
	default:
		return nil, errors.New("Invalid Agent Type")
	}
}
