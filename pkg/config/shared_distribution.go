package config

import (
	types "github.com/chainreactors/cyber/core/types"
	"google.golang.org/protobuf/proto"
)

// SharedFromOption projects the host options into the canonical distributed
// shape. It is used to distinguish provider-only reloads from graph changes.
func SharedFromOption(option *Option) (*types.DistributeConfig, error) {
	if option == nil {
		return &types.DistributeConfig{}, nil
	}
	extensions, err := ValuesToProto(option.Extensions)
	if err != nil {
		return nil, err
	}
	return &types.DistributeConfig{
		Llm: LLMFromOption(option),
		Agent: &types.AgentConfig{
			Tools: append([]string(nil), option.Tools...), Timeout: proto.Int32(int32(option.Timeout)),
			Heartbeat: int32(option.Heartbeat), EvalCriteria: option.EvalCriteria, EvalModel: option.EvalModel,
			EvalRounds: option.EvalRounds, CaptureProviderFrames: option.CaptureProviderFrames,
		},
		Traffic: &types.TrafficConfig{
			BodyStorage: option.BodyStorage, BodyMaxBytes: option.BodyMaxBytes,
			BodyRetentionBytes: option.BodyRetentionBytes,
		},
		Node:       &types.NodeConfig{Id: option.NodeID, Name: option.NodeName},
		Extensions: extensions,
	}, nil
}
