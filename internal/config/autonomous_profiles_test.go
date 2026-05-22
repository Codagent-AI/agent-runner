package config

import "testing"

func TestAutonomousDefaultModeValidation(t *testing.T) {
	cfg, err := buildConfig(defaultParsedFile(), nil, &parsedFile{
		ActiveProfile: "project",
		Profiles: map[string]*ProfileSet{
			"project": {
				Agents: map[string]*Agent{
					"worker": {DefaultMode: "autonomous", CLI: "claude"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("buildConfig returned error: %v", err)
	}
	if err := cfg.validate(); err != nil {
		t.Fatalf("validate returned error: %v", err)
	}
}

func TestDefaultProfileSetUsesAutonomousBase(t *testing.T) {
	cfg, err := buildConfig(defaultParsedFile(), nil, nil)
	if err != nil {
		t.Fatalf("buildConfig returned error: %v", err)
	}
	if err := cfg.validate(); err != nil {
		t.Fatalf("validate returned error: %v", err)
	}

	autonomousBase := cfg.ActiveAgents["autonomous_base"]
	if autonomousBase == nil {
		t.Fatal("expected autonomous_base in default profile")
	}
	if autonomousBase.DefaultMode != "autonomous" || autonomousBase.CLI != "claude" || autonomousBase.Model != "opus" || autonomousBase.Effort != "high" {
		t.Fatalf("unexpected autonomous_base profile: %+v", autonomousBase)
	}

	implementor, err := cfg.Resolve("implementor")
	if err != nil {
		t.Fatalf("Resolve(implementor) returned error: %v", err)
	}
	if implementor.DefaultMode != "autonomous" || implementor.CLI != "claude" || implementor.Model != "opus" || implementor.Effort != "high" {
		t.Fatalf("unexpected implementor profile: %+v", implementor)
	}

	summarizer, err := cfg.Resolve("summarizer")
	if err != nil {
		t.Fatalf("Resolve(summarizer) returned error: %v", err)
	}
	if summarizer.DefaultMode != "autonomous" || summarizer.CLI != "claude" || summarizer.Model != "haiku" || summarizer.Effort != "low" {
		t.Fatalf("unexpected summarizer profile: %+v", summarizer)
	}

	if cfg.ActiveAgents["headless_base"] != nil {
		t.Fatal("did not expect legacy headless_base in default profile")
	}
}
