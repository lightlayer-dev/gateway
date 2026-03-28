package detection

// AgentInfo describes a detected AI agent.
type AgentInfo struct {
	Detected bool
	Name     string
	Provider string
	Version  string
	Verified bool
}
