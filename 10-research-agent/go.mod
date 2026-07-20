module github.com/arixbit/agent-series/10-research-agent

go 1.23

require github.com/arixbit/agent-series/agent v0.0.0

require (
	github.com/dlclark/regexp2 v1.8.1 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/pkoukk/tiktoken-go v0.1.0 // indirect
	github.com/sashabaranov/go-openai v1.41.0 // indirect
)

replace github.com/arixbit/agent-series/agent => ../agent
