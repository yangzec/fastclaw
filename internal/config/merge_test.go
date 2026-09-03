package config

import "testing"

func TestMergedAgentConfigInheritsAndOverridesMCP(t *testing.T) {
	prev := AgentFileConfigLoader
	t.Cleanup(func() { AgentFileConfigLoader = prev })

	AgentFileConfigLoader = func(agentID, _ string) (AgentFileConfig, bool) {
		if agentID != "agt" {
			return AgentFileConfig{}, false
		}
		return AgentFileConfig{
			MCPServers: map[string]MCPServerConfig{
				"shared": {Type: "http", URL: "https://agent.example/mcp"},
				"local":  {Type: "stdio", Command: "npx"},
				"off":    {Type: "http", URL: "https://keep.example", Disabled: true},
			},
		}, true
	}

	cfg := &Config{
		MCPServers: map[string]MCPServerConfig{
			"shared":      {Type: "http", URL: "https://global.example/mcp", Inherit: InheritAll},
			"off":         {Type: "http", URL: "https://global.example/off", Inherit: InheritAll},
			"only-global": {Type: "http", URL: "https://global.example/only", Inherit: InheritAll},
			"catalog":     {Type: "http", URL: "https://global.example/hidden"},
		},
	}
	got := cfg.MergedAgentConfig(AgentEntry{ID: "agt"})

	if got.MCPServers["shared"].URL != "https://agent.example/mcp" {
		t.Fatalf("agent overlay should win for shared, got %+v", got.MCPServers["shared"])
	}
	if got.MCPServers["only-global"].URL != "https://global.example/only" {
		t.Fatalf("global-only server should inherit, got %+v", got.MCPServers["only-global"])
	}
	if got.MCPServers["local"].Command != "npx" {
		t.Fatalf("agent-only server missing: %+v", got.MCPServers["local"])
	}
	if !got.MCPServers["off"].Disabled {
		t.Fatal("agent disabled overlay should hide inherited server")
	}
	if _, ok := got.MCPServers["catalog"]; ok {
		t.Fatal("inherit=none catalog server must not attach to agents")
	}
}

func TestSkillInheritsToAgents(t *testing.T) {
	if SkillInheritsToAgents(SkillEntryCfg{}) {
		t.Fatal("missing / empty inherit must stay catalog-only")
	}
	if !SkillInheritsToAgents(SkillEntryCfg{Inherit: InheritAll}) {
		t.Fatal("inherit=all must attach")
	}
	if SkillInheritsToAgents(SkillEntryCfg{Inherit: InheritNone}) {
		t.Fatal("inherit=none must stay catalog-only")
	}
}
